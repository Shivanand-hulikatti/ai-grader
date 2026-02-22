package grading

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Shivanand-hulikatti/ai-grader/internal/models"
)

type OpenRouterClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewOpenRouterClient(apiKey, model string) *OpenRouterClient {
	if model == "" {
		model = "google/gemini-2.0-flash-001"
	}

	return &OpenRouterClient{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

type openRouterChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openRouterMessage `json:"messages"`
	Temperature float64             `json:"temperature"`
	ResponseFmt map[string]string   `json:"response_format,omitempty"`
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterChatResponse struct {
	Choices []openRouterChoice `json:"choices"`
}

type openRouterChoice struct {
	Message openRouterMessage `json:"message"`
}

func (c *OpenRouterClient) GradeWithRubric(ctx context.Context, rubric, answerText string, maxScore int) (models.GradingFeedback, error) {
	if c.apiKey == "" {
		return models.GradingFeedback{}, fmt.Errorf("openrouter api key is required")
	}

	systemPrompt := "You are an examiner. Grade the student's answer strictly using the provided rubric and return JSON only."
	userPrompt := fmt.Sprintf(`RUBRIC:\n%s\n\nMAX_SCORE: %d\n\nSTUDENT_ANSWER:\n%s\n\nReturn strictly valid JSON matching this schema:\n{\n  "overall_score": number,\n  "summary": string,\n  "criteria": [\n    {"name": string, "score": number, "comment": string}\n  ]\n}\nNo markdown, no extra fields.`, rubric, maxScore, answerText)

	payload := openRouterChatRequest{
		Model: c.model,
		Messages: []openRouterMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.1,
		ResponseFmt: map[string]string{"type": "json_object"},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return models.GradingFeedback{}, fmt.Errorf("failed to marshal openrouter request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return models.GradingFeedback{}, fmt.Errorf("failed to build openrouter request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://ai-grader.local")
	req.Header.Set("X-Title", "ai-grader")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return models.GradingFeedback{}, fmt.Errorf("openrouter request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return models.GradingFeedback{}, fmt.Errorf("failed to read openrouter response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return models.GradingFeedback{}, fmt.Errorf("openrouter api returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed openRouterChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return models.GradingFeedback{}, fmt.Errorf("failed to parse openrouter envelope: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return models.GradingFeedback{}, fmt.Errorf("openrouter returned no choices")
	}

	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var feedback models.GradingFeedback
	if err := json.Unmarshal([]byte(content), &feedback); err != nil {
		return models.GradingFeedback{}, fmt.Errorf("failed to parse grading json: %w", err)
	}

	if feedback.OverallScore < 0 {
		feedback.OverallScore = 0
	}
	if feedback.OverallScore > maxScore {
		feedback.OverallScore = maxScore
	}

	return feedback, nil
}
