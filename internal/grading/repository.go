package grading

import (
	"context"

	"github.com/Shivanand-hulikatti/ai-grader/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
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
