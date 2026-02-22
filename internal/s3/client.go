package s3

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

type Client struct {
	s3Client   *s3.Client
	bucketName string
}

// NewClient creates a new S3 client
func NewClient(region, accessKeyID, secretAccessKey, bucketName string) (*Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKeyID,
			secretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &Client{
		s3Client:   s3.NewFromConfig(cfg),
		bucketName: bucketName,
	}, nil
}

// UploadFile uploads a file to S3 and returns the S3 key
// S3 key pattern: uploads/{YYYY-MM-DD}/{uuid}.pdf
func (c *Client) UploadFile(ctx context.Context, fileReader io.Reader, contentType string, fileSize int64) (string, error) {
	// Generate S3 key with date-based organization
	now := time.Now()
	dateFolder := now.Format("2006-01-02")
	fileID := uuid.New().String()
	s3Key := fmt.Sprintf("uploads/%s/%s.pdf", dateFolder, fileID)

	// Upload to S3 with server-side encryption
	_, err := c.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:               aws.String(c.bucketName),
		Key:                  aws.String(s3Key),
		ContentType:          aws.String(contentType),
		ContentLength:        aws.Int64(fileSize),
		ServerSideEncryption: "AES256",
		Body:                 fileReader,
		Metadata: map[string]string{
			"uploaded-at": now.Format(time.RFC3339),
		},
	})

	if err != nil {
		return "", fmt.Errorf("failed to upload file to S3: %w", err)
	}

	return s3Key, nil
}

// GetSignedURL generates a pre-signed URL for downloading a file (expires in 1 hour)
func (c *Client) GetSignedURL(ctx context.Context, s3Key string, expiresIn time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(c.s3Client)

	request, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucketName),
		Key:    aws.String(s3Key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiresIn
	})

	if err != nil {
		return "", fmt.Errorf("failed to generate signed URL: %w", err)
	}

	return request.URL, nil
}

// DeleteFile deletes a file from S3
func (c *Client) DeleteFile(ctx context.Context, s3Key string) error {
	_, err := c.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucketName),
		Key:    aws.String(s3Key),
	})

	if err != nil {
		return fmt.Errorf("failed to delete file from S3: %w", err)
	}

	return nil
}

// DownloadFile downloads file content from S3.
func (c *Client) DownloadFile(ctx context.Context, s3Key string) ([]byte, error) {
	result, err := c.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucketName),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to download file from S3: %w", err)
	}
	defer result.Body.Close()

	content, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read S3 object body: %w", err)
	}

	return content, nil
}
