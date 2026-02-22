package grading

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Shivanand-hulikatti/ai-grader/internal/models"
	"github.com/Shivanand-hulikatti/ai-grader/internal/pdf"
	"github.com/Shivanand-hulikatti/ai-grader/internal/s3"
)

type Service struct {
	repo   *Repository
	s3     *s3.Client
	parser *pdf.Parser
	llm    *OpenRouterClient
	rubric string
}

func NewService(repo *Repository, s3Client *s3.Client, parser *pdf.Parser, llm *OpenRouterClient, rubric string) *Service {
	return &Service{
		repo:   repo,
		s3:     s3Client,
		parser: parser,
		llm:    llm,
		rubric: strings.TrimSpace(rubric),
	}
}

func (s *Service) HandlePaperUploaded(ctx context.Context, event models.PaperUploadedEvent) error {
	if event.SubmissionID == "" || event.S3Key == "" {
		return fmt.Errorf("invalid event payload: missing submission_id or s3_key")
	}

	submission, err := s.repo.GetSubmissionForGrading(ctx, event.SubmissionID)
	if err != nil {
		return fmt.Errorf("failed to load submission for grading: %w", err)
	}
	if submission == nil {
		return fmt.Errorf("submission not found: %s", event.SubmissionID)
	}

	rubric := strings.TrimSpace(submission.AnswerScheme)
	if rubric == "" {
		rubric = s.rubric
	}
	if rubric == "" {
		return fmt.Errorf("no grading rubric provided for submission %s", event.SubmissionID)
	}

	maxScore := submission.MaxScore
	if maxScore <= 0 {
		maxScore = 100
	}

	if err := s.repo.MarkSubmissionProcessing(ctx, event.SubmissionID); err != nil {
		return fmt.Errorf("failed to mark submission as processing: %w", err)
	}

	pdfContent, err := s.s3.DownloadFile(ctx, event.S3Key)
	if err != nil {
		s.failSubmission(ctx, event.SubmissionID, fmt.Sprintf("failed to download PDF from storage: %v", err))
		return err
	}

	text, err := s.parser.ExtractText(ctx, pdfContent)
	if err != nil {
		s.failSubmission(ctx, event.SubmissionID, fmt.Sprintf("failed to extract text from PDF: %v", err))
		return err
	}

	feedback, err := s.llm.GradeWithRubric(ctx, rubric, text, maxScore)
	if err != nil {
		s.failSubmission(ctx, event.SubmissionID, fmt.Sprintf("failed to grade answer with model: %v", err))
		return err
	}

	feedbackJSON, err := json.Marshal(feedback)
	if err != nil {
		s.failSubmission(ctx, event.SubmissionID, fmt.Sprintf("failed to serialize feedback JSON: %v", err))
		return err
	}

	grade := &models.Grade{
		SubmissionID: event.SubmissionID,
		Score:        feedback.OverallScore,
		Feedback:     string(feedbackJSON),
	}

	gradedEvent := models.PaperGradedEvent{
		SubmissionID: event.SubmissionID,
		Feedback:     feedback,
	}

	if err := s.repo.SaveGradeAndEvent(ctx, grade, gradedEvent); err != nil {
		s.failSubmission(ctx, event.SubmissionID, fmt.Sprintf("failed to save grade and outbox event: %v", err))
		return err
	}

	return nil
}

func (s *Service) failSubmission(ctx context.Context, submissionID, errorMessage string) {
	_ = s.repo.MarkSubmissionFailed(ctx, submissionID, errorMessage)
}
