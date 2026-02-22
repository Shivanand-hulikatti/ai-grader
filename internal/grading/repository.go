package grading

import (
	"context"
	"fmt"

	"github.com/Shivanand-hulikatti/ai-grader/internal/kafka"
	"github.com/Shivanand-hulikatti/ai-grader/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db     *pgxpool.Pool
	outbox *kafka.OutboxRepository
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{
		db:     db,
		outbox: kafka.NewOutboxRepository(db),
	}
}

// CreateGrade inserts a new grade
func (r *Repository) CreateGrade(ctx context.Context, grade *models.Grade) error {
	query := `
        INSERT INTO grades (submission_id, score, feedback)
        VALUES ($1, $2, $3)
        RETURNING id, graded_at, created_at
    `

	return r.db.QueryRow(ctx, query,
		grade.SubmissionID,
		grade.Score,
		grade.Feedback,
	).Scan(&grade.ID, &grade.GradedAt, &grade.CreatedAt)
}

// GetGradeBySubmissionID retrieves grade for a submission
func (r *Repository) GetGradeBySubmissionID(ctx context.Context, submissionID string) (*models.Grade, error) {
	query := `
        SELECT id, submission_id, score, feedback, graded_at, created_at
        FROM grades
        WHERE submission_id = $1
    `

	var grade models.Grade
	err := r.db.QueryRow(ctx, query, submissionID).Scan(
		&grade.ID,
		&grade.SubmissionID,
		&grade.Score,
		&grade.Feedback,
		&grade.GradedAt,
		&grade.CreatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &grade, nil
}

func (r *Repository) MarkSubmissionProcessing(ctx context.Context, submissionID string) error {
	query := `
        UPDATE submissions
        SET status = 'processing', error_message = NULL, updated_at = NOW()
        WHERE id = $1
    `

	_, err := r.db.Exec(ctx, query, submissionID)
	return err
}

func (r *Repository) MarkSubmissionFailed(ctx context.Context, submissionID, errorMessage string) error {
	query := `
        UPDATE submissions
        SET status = 'failed', error_message = $1, updated_at = NOW()
        WHERE id = $2
    `

	_, err := r.db.Exec(ctx, query, errorMessage, submissionID)
	return err
}

func (r *Repository) SaveGradeAndEvent(ctx context.Context, grade *models.Grade, eventPayload models.PaperGradedEvent) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	insertGradeQuery := `
        INSERT INTO grades (submission_id, score, feedback)
        VALUES ($1, $2, $3)
        RETURNING id, graded_at, created_at
    `

	err = tx.QueryRow(ctx, insertGradeQuery,
		grade.SubmissionID,
		grade.Score,
		grade.Feedback,
	).Scan(&grade.ID, &grade.GradedAt, &grade.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create grade: %w", err)
	}

	updateSubmissionQuery := `
        UPDATE submissions
        SET status = 'graded', error_message = NULL, updated_at = NOW()
        WHERE id = $1
    `

	if _, err := tx.Exec(ctx, updateSubmissionQuery, grade.SubmissionID); err != nil {
		return fmt.Errorf("failed to update submission status: %w", err)
	}

	eventPayload.GradeID = grade.ID
	eventPayload.Score = grade.Score
	eventPayload.Status = "graded"

	if err := r.outbox.CreateEventInTransaction(ctx, tx, grade.SubmissionID, "paper-graded", eventPayload); err != nil {
		return fmt.Errorf("failed to create outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (r *Repository) GetSubmissionForGrading(ctx context.Context, submissionID string) (*models.Submission, error) {
	query := `
        SELECT id, user_id, roll_no, course, max_score, answer_scheme,
               s3_key, file_size, status, error_message, created_at, updated_at
        FROM submissions
        WHERE id = $1
    `

	var sub models.Submission
	err := r.db.QueryRow(ctx, query, submissionID).Scan(
		&sub.ID,
		&sub.UserID,
		&sub.RollNo,
		&sub.Course,
		&sub.MaxScore,
		&sub.AnswerScheme,
		&sub.S3Key,
		&sub.FileSize,
		&sub.Status,
		&sub.ErrorMessage,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &sub, nil
}
