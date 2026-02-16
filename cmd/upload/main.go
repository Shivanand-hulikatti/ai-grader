package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Shivanand-hulikatti/ai-grader/internal/database"
	"github.com/Shivanand-hulikatti/ai-grader/internal/kafka"
	"github.com/Shivanand-hulikatti/ai-grader/internal/models"
	"github.com/Shivanand-hulikatti/ai-grader/internal/pdf"
	"github.com/Shivanand-hulikatti/ai-grader/internal/s3"
	"github.com/Shivanand-hulikatti/ai-grader/internal/upload"
	"github.com/joho/godotenv"
)

var (
	s3Client        *s3.Client
	uploadService   *upload.Service
	outboxPublisher *kafka.OutboxPublisher
	kafkaProducer   *kafka.Producer
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	uploadServicePort := os.Getenv("UPLOAD_SERVICE_PORT")
	if uploadServicePort == "" {
		uploadServicePort = "8081"
	}

	// Connect to db
	if err := database.Connect(); err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer database.Close()

	// Initialize S3 client
	awsRegion := os.Getenv("AWS_REGION")
	awsAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	awsSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	s3BucketName := os.Getenv("S3_BUCKET_NAME")

	if awsRegion == "" || awsAccessKey == "" || awsSecretKey == "" || s3BucketName == "" {
		log.Fatal("AWS credentials not configured. Please set AWS_REGION, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, and S3_BUCKET_NAME")
	}

	var err error
	s3Client, err = s3.NewClient(awsRegion, awsAccessKey, awsSecretKey, s3BucketName)
	if err != nil {
		log.Fatal("Failed to initialize S3 client:", err)
	}
	log.Println("S3 client initialized successfully")

	// Initialize upload service
	uploadService = upload.NewService(database.Pool)

	// Initialize Kafka producer
	kafkaBrokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	if len(kafkaBrokers) == 0 || kafkaBrokers[0] == "" {
		kafkaBrokers = []string{"localhost:9092"}
	}
	kafkaProducer = kafka.NewProducer(kafkaBrokers)
	defer kafkaProducer.Close()
	log.Printf("Kafka producer initialized with brokers: %v", kafkaBrokers)

	// Initialize outbox publisher
	outboxPublisher = kafka.NewOutboxPublisher(database.Pool, kafkaProducer)

	// Start outbox publisher in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kafkaTopic := os.Getenv("KAFKA_TOPIC")
	if kafkaTopic == "" {
		kafkaTopic = "paper-uploaded"
	}

	go outboxPublisher.Start(ctx, kafkaTopic)
	log.Printf("Outbox publisher started, publishing to topic: %s", kafkaTopic)

	// Setup HTTP handlers
	http.HandleFunc("POST /upload", withTimeout(withRecovery(handleUpload)))
	http.HandleFunc("GET /health", handleHealth)

	// Setup graceful shutdown
	server := &http.Server{
		Addr:         ":" + uploadServicePort,
		Handler:      http.DefaultServeMux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Upload service listening on port %s", uploadServicePort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server failed:", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited properly")
}

// handleUpload processes file uploads
func handleUpload(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from header (set by API gateway)
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		respondError(w, http.StatusUnauthorized, "unauthorized", "User authentication required")
		return
	}

	// Validate content type
	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		respondError(w, http.StatusBadRequest, "invalid_content_type", "Content-Type must be multipart/form-data")
		return
	}

	// Parse multipart form with 25MB max memory
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", "Failed to parse multipart form: "+err.Error())
		return
	}

	// Extract file from form
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "missing_file", "File is required in 'file' field")
		return
	}
	defer file.Close()

	// Validate file size
	fileSize := fileHeader.Size
	if err := pdf.ValidateFileSize(fileSize); err != nil {
		respondError(w, http.StatusBadRequest, "file_too_large", err.Error())
		return
	}

	// Read file content for validation
	fileContent, err := io.ReadAll(file)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "read_error", "Failed to read file content")
		return
	}

	// Validate PDF magic bytes
	if err := pdf.ValidatePDF(bytes.NewReader(fileContent)); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_pdf", "File must be a valid PDF document")
		return
	}

	// Extract optional form fields
	rollNo := r.FormValue("roll_no")
	course := r.FormValue("course")
	answerScheme := r.FormValue("answer_scheme")

	maxScore := 100
	if maxScoreStr := r.FormValue("max_score"); maxScoreStr != "" {
		if parsed, err := strconv.Atoi(maxScoreStr); err == nil && parsed > 0 {
			maxScore = parsed
		}
	}

	// Upload to S3
	ctx := r.Context()
	s3Key, err := s3Client.UploadFile(ctx, bytes.NewReader(fileContent), "application/pdf", fileSize)
	if err != nil {
		log.Printf("S3 upload failed for user %s: %v", userID, err)
		respondError(w, http.StatusInternalServerError, "upload_failed", "Failed to upload file to storage")
		return
	}

	log.Printf("File uploaded to S3: %s (user: %s, size: %d bytes)", s3Key, userID, fileSize)

	// Create submission with outbox event
	submission := &models.Submission{
		UserID:       userID,
		RollNo:       rollNo,
		Course:       course,
		MaxScore:     maxScore,
		AnswerScheme: answerScheme,
		S3Key:        s3Key,
		FileSize:     fileSize,
		Status:       "uploaded",
	}

	if err := uploadService.CreateSubmissionWithEvent(ctx, submission); err != nil {
		log.Printf("Failed to create submission for user %s: %v", userID, err)

		// Attempt to clean up S3 file
		if delErr := s3Client.DeleteFile(ctx, s3Key); delErr != nil {
			log.Printf("Failed to clean up S3 file %s: %v", s3Key, delErr)
		}

		respondError(w, http.StatusInternalServerError, "database_error", "Failed to create submission record")
		return
	}

	log.Printf("Submission created: %s (user: %s)", submission.ID, userID)

	// Return success response
	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"submission_id": submission.ID,
		"status":        submission.Status,
		"s3_key":        submission.S3Key,
		"file_size":     submission.FileSize,
		"created_at":    submission.CreatedAt,
	})
}

// handleHealth returns service health status
func handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "healthy",
		"service": "upload-service",
	})
}

// respondJSON sends a JSON response
func respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// respondError sends an error response in JSON format
func respondError(w http.ResponseWriter, statusCode int, errorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   errorCode,
		"message": message,
	})
}

// withTimeout adds a timeout to the request context
func withTimeout(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		next(w, r.WithContext(ctx))
	}
}

// withRecovery recovers from panics and returns 500 error
func withRecovery(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("Panic recovered: %v", err)
				respondError(w, http.StatusInternalServerError, "internal_error", "An unexpected error occurred")
			}
		}()
		next(w, r)
	}
}
