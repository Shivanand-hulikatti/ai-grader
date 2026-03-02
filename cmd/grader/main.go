package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Shivanand-hulikatti/ai-grader/internal/database"
	"github.com/Shivanand-hulikatti/ai-grader/internal/grading"
	"github.com/Shivanand-hulikatti/ai-grader/internal/kafka"
	"github.com/Shivanand-hulikatti/ai-grader/internal/models"
	"github.com/Shivanand-hulikatti/ai-grader/internal/pdf"
	"github.com/Shivanand-hulikatti/ai-grader/internal/s3"
	"github.com/joho/godotenv"
)

type uploadedEventEnvelope struct {
	SubmissionID string                   `json:"submission_id"`
	S3Key        string                   `json:"s3_key"`
	UserID       string                   `json:"user_id"`
	Submission   *uploadedEventSubmission `json:"submission"`
	S3           *uploadedEventS3         `json:"s3"`
	Payload      json.RawMessage          `json:"payload"`
}

type uploadedEventSubmission struct {
	ID string `json:"id"`
}

type uploadedEventS3 struct {
	Key string `json:"key"`
}

func parsePaperUploadedEvent(message []byte) (models.PaperUploadedEvent, bool) {
	var direct models.PaperUploadedEvent
	if err := json.Unmarshal(message, &direct); err == nil {
		if direct.SubmissionID != "" && direct.S3Key != "" {
			return direct, true
		}
	}

	var env uploadedEventEnvelope
	if err := json.Unmarshal(message, &env); err != nil {
		return models.PaperUploadedEvent{}, false
	}

	event := models.PaperUploadedEvent{
		SubmissionID: env.SubmissionID,
		S3Key:        env.S3Key,
		UserID:       env.UserID,
	}

	if event.SubmissionID == "" && env.Submission != nil {
		event.SubmissionID = env.Submission.ID
	}
	if event.S3Key == "" && env.S3 != nil {
		event.S3Key = env.S3.Key
	}

	if event.SubmissionID != "" && event.S3Key != "" {
		return event, true
	}

	if len(env.Payload) > 0 {
		var nested models.PaperUploadedEvent
		if err := json.Unmarshal(env.Payload, &nested); err == nil {
			if nested.SubmissionID != "" && nested.S3Key != "" {
				return nested, true
			}
		}

		var nestedEnv uploadedEventEnvelope
		if err := json.Unmarshal(env.Payload, &nestedEnv); err == nil {
			nestedEvent := models.PaperUploadedEvent{
				SubmissionID: nestedEnv.SubmissionID,
				S3Key:        nestedEnv.S3Key,
				UserID:       nestedEnv.UserID,
			}
			if nestedEvent.SubmissionID == "" && nestedEnv.Submission != nil {
				nestedEvent.SubmissionID = nestedEnv.Submission.ID
			}
			if nestedEvent.S3Key == "" && nestedEnv.S3 != nil {
				nestedEvent.S3Key = nestedEnv.S3.Key
			}
			if nestedEvent.SubmissionID != "" && nestedEvent.S3Key != "" {
				return nestedEvent, true
			}
		}
	}

	return models.PaperUploadedEvent{}, false
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	if err := database.Connect(); err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer database.Close()

	kafkaBrokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	if len(kafkaBrokers) == 0 || kafkaBrokers[0] == "" {
		kafkaBrokers = []string{"localhost:9092"}
	}

	uploadedTopic := os.Getenv("KAFKA_TOPIC")
	if uploadedTopic == "" {
		uploadedTopic = "paper-uploaded"
	}

	gradedTopic := os.Getenv("KAFKA_GRADED_TOPIC")
	if gradedTopic == "" {
		gradedTopic = "paper-graded"
	}

	groupID := os.Getenv("KAFKA_CONSUMER_GROUP_ID")
	if groupID == "" {
		groupID = "grader-consumer-group"
	}

	awsRegion := os.Getenv("AWS_REGION")
	awsAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	awsSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	s3BucketName := os.Getenv("S3_BUCKET_NAME")

	if awsRegion == "" || awsAccessKey == "" || awsSecretKey == "" || s3BucketName == "" {
		log.Fatal("AWS credentials not configured. Please set AWS_REGION, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, and S3_BUCKET_NAME")
	}

	s3Client, err := s3.NewClient(awsRegion, awsAccessKey, awsSecretKey, s3BucketName)
	if err != nil {
		log.Fatal("Failed to initialize S3 client:", err)
	}

	openRouterKey := os.Getenv("OPENROUTER_API_KEY")
	openRouterModel := os.Getenv("OPENROUTER_MODEL") // defaults to qwen3-vl-235b if empty
	globalRubric := os.Getenv("GLOBAL_GRADING_RUBRIC")

	if openRouterKey == "" || strings.TrimSpace(globalRubric) == "" {
		log.Fatal("Missing required env vars: OPENROUTER_API_KEY, GLOBAL_GRADING_RUBRIC")
	}

	repo := grading.NewRepository(database.Pool)
	renderer := pdf.NewRenderer()
	llm := grading.NewOpenRouterClient(openRouterKey, openRouterModel)
	service := grading.NewService(repo, s3Client, renderer, llm, globalRubric)

	consumer := kafka.NewConsumer(kafkaBrokers, uploadedTopic, groupID)
	defer consumer.Close()

	producer := kafka.NewProducer(kafkaBrokers)
	defer producer.Close()
	outboxPublisher := kafka.NewOutboxPublisher(database.Pool, producer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go outboxPublisher.Start(ctx, gradedTopic)
	log.Printf("Outbox publisher started for topic: %s", gradedTopic)

	go func() {
		err := consumer.Start(ctx, func(msgCtx context.Context, message []byte) error {
			event, ok := parsePaperUploadedEvent(message)
			if !ok {
				log.Printf("Skipping malformed/legacy paper-uploaded event")
				return nil
			}

			if err := service.HandlePaperUploaded(msgCtx, event); err != nil {
				return err
			}

			log.Printf("Processed grading for submission: %s", event.SubmissionID)
			return nil
		})
		if err != nil {
			log.Printf("Kafka consumer stopped with error: %v", err)
			cancel()
		}
	}()

	log.Println("Grader service started (Vision-LLM mode)")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down grader service...")
	cancel()
}
