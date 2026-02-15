package kafka

import (
	"context"
	"encoding/json"

	"github.com/Shivanand-hulikatti/ai-grader/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxRepository struct {
	db *pgxpool.Pool
}

func NewOutboxRepository(db *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{db: db}
}

// CreateEventInTransaction creates an outbox event within a transaction
func (r *OutboxRepository) CreateEventInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	submissionID, eventType string,
	payload interface{},
) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	query := `
        INSERT INTO outbox_events (submission_id, event_type, payload, status)
        VALUES ($1, $2, $3, 'pending')
    `

	_, err = tx.Exec(ctx, query, submissionID, eventType, string(payloadJSON))
	return err
}

// GetPendingEvents retrieves unpublished events
func (r *OutboxRepository) GetPendingEvents(ctx context.Context, limit int) ([]*models.OutboxEvent, error) {
	query := `
        SELECT id, submission_id, event_type, payload, status, attempt_no, error, created_at
        FROM outbox_events
        WHERE status = 'pending'
        ORDER BY created_at ASC
        LIMIT $1
    `

	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*models.OutboxEvent
	for rows.Next() {
		var event models.OutboxEvent
		err := rows.Scan(
			&event.ID,
			&event.SubmissionID,
			&event.EventType,
			&event.Payload,
			&event.Status,
			&event.AttemptNo,
			&event.Error,
			&event.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		events = append(events, &event)
	}

	return events, rows.Err()
}

// MarkEventPublished marks an event as published
func (r *OutboxRepository) MarkEventPublished(ctx context.Context, eventID string) error {
	query := `
        UPDATE outbox_events
        SET status = 'published', published_at = NOW()
        WHERE id = $1
    `

	_, err := r.db.Exec(ctx, query, eventID)
	return err
}

// IncrementAttempt increments the attempt counter
func (r *OutboxRepository) IncrementAttempt(ctx context.Context, eventID string, errorMsg string) error {
	query := `
        UPDATE outbox_events
        SET attempt_no = attempt_no + 1, error = $1
        WHERE id = $2
    `

	_, err := r.db.Exec(ctx, query, errorMsg, eventID)
	return err
}

// MarkEventFailed marks an event as failed
func (r *OutboxRepository) MarkEventFailed(ctx context.Context, eventID string, errorMsg string) error {
	query := `
        UPDATE outbox_events
        SET status = 'failed', error = $1
        WHERE id = $2
    `

	_, err := r.db.Exec(ctx, query, errorMsg, eventID)
	return err
}
