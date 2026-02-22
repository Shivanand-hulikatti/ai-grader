package pdf

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Parser struct {
	apiKey     string
	httpClient *http.Client
}

func NewParser(apiKey string) *Parser {
	return &Parser{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type visionRequest struct {
	Requests []visionAnnotateFileRequest `json:"requests"`
}

type visionAnnotateFileRequest struct {
	InputConfig visionInputConfig `json:"inputConfig"`
	Features    []visionFeature   `json:"features"`
	Pages       []int             `json:"pages,omitempty"`
}

type visionInputConfig struct {
	MimeType string `json:"mimeType"`
	Content  string `json:"content"`
}

type visionFeature struct {
	Type string `json:"type"`
}

type visionResponse struct {
	Responses []visionFileAnnotateResponse `json:"responses"`
}

type visionFileAnnotateResponse struct {
	Responses []visionPageResponse `json:"responses"`
	Error     *visionError         `json:"error,omitempty"`
}

type visionPageResponse struct {
	FullTextAnnotation *visionTextAnnotation `json:"fullTextAnnotation,omitempty"`
	Error              *visionError          `json:"error,omitempty"`
}

type visionTextAnnotation struct {
	Text string `json:"text"`
}

type visionError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (p *Parser) ExtractText(ctx context.Context, pdfContent []byte) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("google vision api key is required")
	}
	if len(pdfContent) == 0 {
		return "", fmt.Errorf("empty pdf content")
	}

	encoded := base64.StdEncoding.EncodeToString(pdfContent)
	payload := visionRequest{
		Requests: []visionAnnotateFileRequest{
			{
				InputConfig: visionInputConfig{
					MimeType: "application/pdf",
					Content:  encoded,
				},
				Features: []visionFeature{{Type: "DOCUMENT_TEXT_DETECTION"}},
				Pages:    []int{1, 2, 3, 4, 5},
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal vision payload: %w", err)
	}

	url := "https://vision.googleapis.com/v1/files:annotate?key=" + p.apiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", fmt.Errorf("failed to build vision request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("google vision request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read vision response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("google vision api returned status %d: %s", resp.StatusCode, string(body))
	}

	var vr visionResponse
	if err := json.Unmarshal(body, &vr); err != nil {
		return "", fmt.Errorf("failed to parse vision response: %w", err)
	}

	var textBuilder strings.Builder
	for _, fileResp := range vr.Responses {
		if fileResp.Error != nil {
			return "", fmt.Errorf("google vision file error: %s", fileResp.Error.Message)
		}
		for _, pageResp := range fileResp.Responses {
			if pageResp.Error != nil {
				return "", fmt.Errorf("google vision page error: %s", pageResp.Error.Message)
			}
			if pageResp.FullTextAnnotation != nil && strings.TrimSpace(pageResp.FullTextAnnotation.Text) != "" {
				if textBuilder.Len() > 0 {
					textBuilder.WriteString("\n")
				}
				textBuilder.WriteString(pageResp.FullTextAnnotation.Text)
			}
		}
	}

	result := strings.TrimSpace(textBuilder.String())
	if result == "" {
		return "", fmt.Errorf("no text extracted from pdf")
	}

	return result, nil
}
