package pdf

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const (
	// MaxFileSize is 25 MB in bytes
	MaxFileSize = 25 * 1024 * 1024 // 26,214,400 bytes

	// PDF magic bytes - all PDFs start with %PDF-
	pdfMagicBytes = "%PDF-"
)

var (
	ErrInvalidPDF       = errors.New("file is not a valid PDF")
	ErrFileTooLarge     = errors.New("file size exceeds 25 MB limit")
	ErrInsufficientData = errors.New("insufficient data to validate PDF")
)

// ValidatePDF checks if the file is a valid PDF by reading magic bytes
func ValidatePDF(reader io.Reader) error {
	// Read first 5 bytes to check for PDF magic signature
	header := make([]byte, 5)
	n, err := io.ReadFull(reader, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		return fmt.Errorf("failed to read file header: %w", err)
	}

	if n < 5 {
		return ErrInsufficientData
	}

	// Check if file starts with %PDF-
	if string(header) != pdfMagicBytes {
		return ErrInvalidPDF
	}

	return nil
}

// ValidateFileSize checks if file size is within limits
func ValidateFileSize(size int64) error {
	if size > MaxFileSize {
		return fmt.Errorf("%w: file is %d bytes, max allowed is %d bytes", ErrFileTooLarge, size, MaxFileSize)
	}

	if size == 0 {
		return errors.New("file is empty")
	}

	return nil
}

// SanitizeFilename removes potentially dangerous characters from filename
func SanitizeFilename(filename string) string {
	// Get just the filename without directory paths
	filename = filepath.Base(filename)

	// Remove any remaining path separators
	filename = strings.ReplaceAll(filename, "/", "")
	filename = strings.ReplaceAll(filename, "\\", "")

	// Remove null bytes
	filename = strings.ReplaceAll(filename, "\x00", "")

	// Limit length to prevent issues
	if len(filename) > 255 {
		ext := filepath.Ext(filename)
		nameWithoutExt := strings.TrimSuffix(filename, ext)
		filename = nameWithoutExt[:255-len(ext)] + ext
	}

	// If filename becomes empty after sanitization, use a default
	if filename == "" {
		filename = "upload.pdf"
	}

	return filename
}

// ValidateContentType checks if the content type is valid for PDF
func ValidateContentType(contentType string) error {
	// Accept application/pdf or empty (will be inferred)
	contentType = strings.ToLower(strings.TrimSpace(contentType))

	if contentType == "" || contentType == "application/pdf" || contentType == "application/octet-stream" {
		return nil
	}

	return fmt.Errorf("invalid content type: %s, expected application/pdf", contentType)
}
