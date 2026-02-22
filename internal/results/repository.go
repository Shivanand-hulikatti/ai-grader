package results

import (
	"context"
	"time"

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

func (r *Repository) GetSubmissionWithGrade(ctx context.Context, submissionID, userID string) (*models.SubmissionResponse, error) {
	query := `
        SELECT s.id, s.user_id, s.roll_no, s.course, s.max_score, s.answer_scheme,
               s.s3_key, s.file_size, s.status, s.error_message, s.created_at, s.updated_at,
               g.id, g.score, g.feedback, g.graded_at, g.created_at
        FROM submissions s
        LEFT JOIN grades g ON g.submission_id = s.id
        WHERE s.id = $1 AND s.user_id = $2
    `

	var sub models.Submission
	var grade models.Grade
	var gradeID *string
	var score *int
	var feedback *string
	var gradedAt *time.Time
	var gradeCreatedAt *time.Time

	err := r.db.QueryRow(ctx, query, submissionID, userID).Scan(
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
		&gradeID,
		&score,
		&feedback,
		&gradedAt,
		&gradeCreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	result := &models.SubmissionResponse{Submission: sub}
	if gradeID != nil && score != nil && feedback != nil {
		grade.ID = *gradeID
		grade.SubmissionID = sub.ID
		grade.Score = *score
		grade.Feedback = *feedback
		if gradedAt != nil {
			grade.GradedAt = *gradedAt
		}
		if gradeCreatedAt != nil {
			grade.CreatedAt = *gradeCreatedAt
		}
		result.Grade = &grade
	}

	return result, nil
}

func (r *Repository) ListSubmissionsWithGrades(ctx context.Context, userID string, limit, offset int) ([]models.SubmissionResponse, error) {
	query := `
        SELECT s.id, s.user_id, s.roll_no, s.course, s.max_score, s.answer_scheme,
               s.s3_key, s.file_size, s.status, s.error_message, s.created_at, s.updated_at,
               g.id, g.score, g.feedback, g.graded_at, g.created_at
        FROM submissions s
        LEFT JOIN grades g ON g.submission_id = s.id
        WHERE s.user_id = $1
        ORDER BY s.created_at DESC
        LIMIT $2 OFFSET $3
    `

	rows, err := r.db.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.SubmissionResponse
	for rows.Next() {
		var sub models.Submission
		var grade models.Grade
		var gradeID *string
		var score *int
		var feedback *string
		var gradedAt *time.Time
		var gradeCreatedAt *time.Time

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
			&gradeID,
			&score,
			&feedback,
			&gradedAt,
			&gradeCreatedAt,
		)
		if err != nil {
			return nil, err
		}

		item := models.SubmissionResponse{Submission: sub}
		if gradeID != nil && score != nil && feedback != nil {
			grade.ID = *gradeID
			grade.SubmissionID = sub.ID
			grade.Score = *score
			grade.Feedback = *feedback
			if gradedAt != nil {
				grade.GradedAt = *gradedAt
			}
			if gradeCreatedAt != nil {
				grade.CreatedAt = *gradeCreatedAt
			}
			item.Grade = &grade
		}

		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
