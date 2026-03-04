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

// Service orchestrates the vision-based grading pipeline:
//  1. Download the student's PDF from S3
//  2. Render each page to a PNG image using the PDF renderer
//  3. Send all images + rubric to the Vision-LLM (OpenRouter)
//  4. Persist the grade and publish an outbox event
type Service struct {
	repo     *Repository
	s3       *s3.Client
	renderer *pdf.Renderer
	llm      *OpenRouterClient
	rubric   string
}

func NewService(repo *Repository, s3Client *s3.Client, renderer *pdf.Renderer, llm *OpenRouterClient, rubric string) *Service {
	return &Service{
		repo:     repo,
		s3:       s3Client,
		renderer: renderer,
		llm:      llm,
		rubric:   strings.TrimSpace(rubric),
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

	// 1. Fetch the PDF bytes from S3.
	pdfContent, err := s.s3.DownloadFile(ctx, event.S3Key)
	if err != nil {
		s.failSubmission(ctx, event.SubmissionID, fmt.Sprintf("failed to download PDF from storage: %v", err))
		return err
	}

	// 2. Render every page to PNG images.
	pageImages, err := s.renderer.RenderPages(pdfContent)
	if err != nil {
		s.failSubmission(ctx, event.SubmissionID, fmt.Sprintf("failed to render PDF pages to images: %v", err))
		return err
	}

	// 3. Send images to the Vision-LLM for grading.
	feedback, err := s.llm.GradeWithImages(ctx, rubric, pageImages, maxScore)
	if err != nil {
		s.failSubmission(ctx, event.SubmissionID, fmt.Sprintf("failed to grade answer sheet with vision model: %v", err))
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

	// 4. Persist grade.
	if err := s.repo.SaveGrade(ctx, grade); err != nil {
		s.failSubmission(ctx, event.SubmissionID, fmt.Sprintf("failed to save grade: %v", err))
		return err
	}

	return nil
}

func (s *Service) failSubmission(ctx context.Context, submissionID, errorMessage string) {
	_ = s.repo.MarkSubmissionFailed(ctx, submissionID, errorMessage)
}
