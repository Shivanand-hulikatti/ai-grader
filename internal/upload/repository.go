package upload

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

// CreateSubmission inserts a new submission
func (r *Repository) CreateSubmission(ctx context.Context, sub *models.Submission) error {
	query := `
        INSERT INTO submissions (
            user_id, roll_no, course, max_score, answer_scheme,
            s3_key, file_size, status
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING id, created_at, updated_at
    `

	return r.db.QueryRow(ctx, query,
		sub.UserID,
		sub.RollNo,
		sub.Course,
		sub.MaxScore,
		sub.AnswerScheme,
		sub.S3Key,
		sub.FileSize,
		sub.Status,
	).Scan(&sub.ID, &sub.CreatedAt, &sub.UpdatedAt)
}

// GetSubmissionByID retrieves a submission by ID
func (r *Repository) GetSubmissionByID(ctx context.Context, id string) (*models.Submission, error) {
	query := `
        SELECT id, user_id, roll_no, course, max_score, answer_scheme,
               s3_key, file_size, status, error_message, created_at, updated_at
        FROM submissions
        WHERE id = $1
    `

	var sub models.Submission
	err := r.db.QueryRow(ctx, query, id).Scan(
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

// UpdateSubmissionStatus updates the status of a submission
func (r *Repository) UpdateSubmissionStatus(ctx context.Context, id, status, errorMsg string) error {
	query := `
        UPDATE submissions
        SET status = $1, error_message = $2, updated_at = NOW()
        WHERE id = $3
    `

	_, err := r.db.Exec(ctx, query, status, errorMsg, id)
	return err
}

// GetUserSubmissions retrieves all submissions for a user
func (r *Repository) GetUserSubmissions(ctx context.Context, userID string) ([]*models.Submission, error) {
	query := `
        SELECT id, user_id, roll_no, course, max_score, answer_scheme,
               s3_key, file_size, status, error_message, created_at, updated_at
        FROM submissions
        WHERE user_id = $1
        ORDER BY created_at DESC
    `

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var submissions []*models.Submission
	for rows.Next() {
		var sub models.Submission
		err := rows.Scan(
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
		if err != nil {
			return nil, err
		}
		submissions = append(submissions, &sub)
	}

	return submissions, rows.Err()
}
