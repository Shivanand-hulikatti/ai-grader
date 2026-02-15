package upload

import (
	"context"
	"fmt"

	"github.com/Shivanand-hulikatti/ai-grader/internal/kafka"
	"github.com/Shivanand-hulikatti/ai-grader/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	repo   *Repository
	outbox *kafka.OutboxRepository
	db     *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{
		repo:   NewRepository(db),
		outbox: kafka.NewOutboxRepository(db),
		db:     db,
	}
}

// CreateSubmissionWithEvent creates submission and outbox event in a transaction
func (s *Service) CreateSubmissionWithEvent(ctx context.Context, sub *models.Submission) error {
	// Begin transaction
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // Rollback if not committed

	// Insert submission
	query := `
        INSERT INTO submissions (
            user_id, roll_no, course, max_score, answer_scheme,
            s3_key, file_size, status
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING id, created_at, updated_at
    `

	err = tx.QueryRow(ctx, query,
		sub.UserID,
		sub.RollNo,
		sub.Course,
		sub.MaxScore,
		sub.AnswerScheme,
		sub.S3Key,
		sub.FileSize,
		"uploaded",
	).Scan(&sub.ID, &sub.CreatedAt, &sub.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert submission: %w", err)
	}

	// Create outbox event
	eventPayload := map[string]interface{}{
		"submission_id": sub.ID,
		"s3_key":        sub.S3Key,
		"user_id":       sub.UserID,
	}

	err = s.outbox.CreateEventInTransaction(ctx, tx, sub.ID, "paper-uploaded", eventPayload)
	if err != nil {
		return fmt.Errorf("failed to create outbox event: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
