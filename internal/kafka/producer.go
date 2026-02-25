package kafka

import (
	"context"
	"fmt"
	"log"

	"github.com/segmentio/kafka-go"
)

type Producer struct {
	writer *kafka.Writer
}

// NewProducer creates a new Kafka producer
func NewProducer(brokers []string) *Producer {
	writer := &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Balancer:               &kafka.Hash{},    // Hash based on key for ordering
		RequiredAcks:           kafka.RequireAll, // Wait for all replicas (strongest durability)
		Async:                  false,            // Synchronous writes for reliability
		Compression:            kafka.Snappy,
		AllowAutoTopicCreation: true, // Let the broker create the topic on first write
	}

	return &Producer{
		writer: writer,
	}
}

// PublishEvent publishes an event to Kafka
func (p *Producer) PublishEvent(ctx context.Context, topic, key string, payload []byte) error {
	message := kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: payload,
	}

	err := p.writer.WriteMessages(ctx, message)
	if err != nil {
		return fmt.Errorf("failed to publish message to kafka: %w", err)
	}

	log.Printf("Published message to topic %s with key %s", topic, key)
	return nil
}

// Close closes the producer
func (p *Producer) Close() error {
	if err := p.writer.Close(); err != nil {
		return fmt.Errorf("failed to close kafka writer: %w", err)
	}
	return nil
}
