package auth

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

// CreateUser inserts a new user
func (r *Repository) CreateUser(ctx context.Context, user *models.User) error {
	query := `
        INSERT INTO users (email, password_hash, full_name, role)
        VALUES ($1, $2, $3, $4)
        RETURNING id, created_at, updated_at
    `

	return r.db.QueryRow(ctx, query,
		user.Email,
		user.PasswordHash,
		user.FullName,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
}

// GetUserByEmail retrieves user by email
func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
        SELECT id, email, password_hash, full_name, role, created_at, updated_at
        FROM users
        WHERE email = $1
    `

	var user models.User
	err := r.db.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FullName,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetUserByID retrieves user by ID
func (r *Repository) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	query := `
        SELECT id, email, password_hash, full_name, role, created_at, updated_at
        FROM users
        WHERE id = $1
    `

	var user models.User
	err := r.db.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FullName,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// SaveRefreshToken stores a refresh token
func (r *Repository) SaveRefreshToken(ctx context.Context, token *models.RefreshToken) error {
	query := `
        INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
        VALUES ($1, $2, $3)
        RETURNING id, created_at
    `

	return r.db.QueryRow(ctx, query,
		token.UserID,
		token.TokenHash,
		token.ExpiresAt,
	).Scan(&token.ID, &token.CreatedAt)
}

// GetRefreshToken retrieves a refresh token
func (r *Repository) GetRefreshToken(ctx context.Context, tokenHash string) (*models.RefreshToken, error) {
	query := `
        SELECT id, user_id, token_hash, expires_at, revoked, created_at
        FROM refresh_tokens
        WHERE token_hash = $1
        AND expires_at > NOW()
        AND revoked = false
    `

	var token models.RefreshToken
	err := r.db.QueryRow(ctx, query, tokenHash).Scan(
		&token.ID,
		&token.UserID,
		&token.TokenHash,
		&token.ExpiresAt,
		&token.Revoked,
		&token.CreatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &token, nil
}

// RevokeRefreshToken marks a token as revoked
func (r *Repository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	query := `
        UPDATE refresh_tokens
        SET revoked = true
        WHERE token_hash = $1
    `

	_, err := r.db.Exec(ctx, query, tokenHash)
	return err
}
