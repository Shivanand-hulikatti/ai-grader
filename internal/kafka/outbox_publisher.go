package kafka

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DefaultPollingInterval = 5 * time.Second
	DefaultBatchSize       = 100
	MaxRetryAttempts       = 5 // Max retry attempts before marking as failed
)

type OutboxPublisher struct {
	db       *pgxpool.Pool
	producer *Producer
	outbox   *OutboxRepository
	interval time.Duration
}

// NewOutboxPublisher creates a new outbox publisher
func NewOutboxPublisher(db *pgxpool.Pool, producer *Producer) *OutboxPublisher {
	return &OutboxPublisher{
		db:       db,
		producer: producer,
		outbox:   NewOutboxRepository(db),
		interval: DefaultPollingInterval,
	}
}

// Start begins polling the outbox table and publishing events
func (op *OutboxPublisher) Start(ctx context.Context, topic string) {
	ticker := time.NewTicker(op.interval)
	defer ticker.Stop()

	log.Printf("Outbox publisher started, polling every %v", op.interval)

	// Run immediately on start
	op.processOutbox(ctx, topic)

	for {
		select {
		case <-ctx.Done():
			log.Println("Outbox publisher shutting down...")
			return
		case <-ticker.C:
			op.processOutbox(ctx, topic)
		}
	}
}

// processOutbox fetches pending events and publishes them to Kafka
func (op *OutboxPublisher) processOutbox(ctx context.Context, topic string) {
	// Fetch pending events
	events, err := op.outbox.GetPendingEvents(ctx, DefaultBatchSize)
	if err != nil {
		log.Printf("Error fetching pending events: %v", err)
		return
	}

	if len(events) == 0 {
		return
	}

	log.Printf("Processing %d pending outbox events", len(events))

	// Process each event
	for _, event := range events {
		// Check if max retries exceeded
		if event.AttemptNo >= MaxRetryAttempts {
			log.Printf("Event %s exceeded max retry attempts, marking as failed", event.ID)
			if err := op.outbox.MarkEventFailed(ctx, event.ID, "exceeded max retry attempts"); err != nil {
				log.Printf("Error marking event as failed: %v", err)
			}
			continue
		}

		// Publish to Kafka
		err := op.producer.PublishEvent(ctx, topic, event.SubmissionID, []byte(event.Payload))
		if err != nil {
			log.Printf("Failed to publish event %s (attempt %d): %v", event.ID, event.AttemptNo+1, err)

			// Increment attempt counter
			if err := op.outbox.IncrementAttempt(ctx, event.ID, err.Error()); err != nil {
				log.Printf("Error incrementing attempt counter: %v", err)
			}
			continue
		}

		// Mark as published
		if err := op.outbox.MarkEventPublished(ctx, event.ID); err != nil {
			log.Printf("Error marking event as published: %v", err)
			// Event was published to Kafka, but DB update failed
			// This is acceptable as Kafka consumers should be idempotent
			continue
		}

		log.Printf("Successfully published event %s (submission_id: %s)", event.ID, event.SubmissionID)
	}
}

// SetPollingInterval allows customizing the polling interval
func (op *OutboxPublisher) SetPollingInterval(interval time.Duration) {
	op.interval = interval
}
