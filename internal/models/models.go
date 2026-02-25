package models

import "time"

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"` // Don't expose in JSON
	FullName     string    `json:"full_name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Submission struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	RollNo       string    `json:"roll_no,omitempty"`
	Course       string    `json:"course,omitempty"`
	MaxScore     int       `json:"max_score"`
	AnswerScheme string    `json:"answer_scheme,omitempty"`
	S3Key        string    `json:"s3_key"`
	FileSize     int64     `json:"file_size"`
	Status       string    `json:"status"`
	ErrorMessage *string   `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Grade struct {
	ID           string    `json:"id"`
	SubmissionID string    `json:"submission_id"`
	Score        int       `json:"score"`
	Feedback     string    `json:"feedback"`
	GradedAt     time.Time `json:"graded_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type OutboxEvent struct {
	ID           string     `json:"id"`
	SubmissionID string     `json:"submission_id"`
	EventType    string     `json:"event_type"`
	Payload      string     `json:"payload"` // JSON string
	Status       string     `json:"status"`
	AttemptNo    int        `json:"attempt_no"`
	Error        *string    `json:"error,omitempty"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type RefreshToken struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TokenHash string    `json:"token_hash"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
}

// Request/Response DTOs
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	User         User   `json:"user"`
}

type SubmissionResponse struct {
	Submission Submission `json:"submission"`
	Grade      *Grade     `json:"grade,omitempty"`
}

type PaperUploadedEvent struct {
	SubmissionID string `json:"submission_id"`
	S3Key        string `json:"s3_key"`
	UserID       string `json:"user_id"`
}

type GradingCriterion struct {
	Name    string `json:"name"`
	Score   int    `json:"score"`
	Comment string `json:"comment"`
}

type GradingFeedback struct {
	OverallScore int                `json:"overall_score"`
	Summary      string             `json:"summary"`
	Criteria     []GradingCriterion `json:"criteria"`
}

type PaperGradedEvent struct {
	SubmissionID string          `json:"submission_id"`
	GradeID      string          `json:"grade_id"`
	Status       string          `json:"status"`
	Score        int             `json:"score"`
	Feedback     GradingFeedback `json:"feedback"`
}
