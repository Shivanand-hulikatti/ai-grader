package results

import (
	"context"

	"github.com/Shivanand-hulikatti/ai-grader/internal/models"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetResultBySubmissionID(ctx context.Context, submissionID, userID string) (*models.SubmissionResponse, error) {
	return s.repo.GetSubmissionWithGrade(ctx, submissionID, userID)
}

func (s *Service) ListResults(ctx context.Context, userID string, limit, offset int) ([]models.SubmissionResponse, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	return s.repo.ListSubmissionsWithGrades(ctx, userID, limit, offset)
}
