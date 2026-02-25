package grading

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

	"github.com/Shivanand-hulikatti/ai-grader/internal/models"
)

// DefaultVisionModel is the OpenRouter model used for vision-based grading.
const DefaultVisionModel = "openai/gpt-5-nano"

// MaxPagesPerSubmission caps how many PDF pages are sent to the LLM per call.
// Keeping this low controls cost and stays within context limits.
const MaxPagesPerSubmission = 10

type OpenRouterClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewOpenRouterClient(apiKey, model string) *OpenRouterClient {
	if model == "" {
		model = DefaultVisionModel
	}

	return &OpenRouterClient{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			// Outer safety net — must be longer than the per-request context timeout
			// set inside GradeWithImages (4 min) so the context always fires first
			// and we get a clean error message instead of a transport-level one.
			Timeout: 5 * time.Minute,
		},
	}
}

// ---------------------------------------------------------------------------
// Wire types for the OpenRouter multimodal chat API
// ---------------------------------------------------------------------------

// visionContentPart represents a single item inside a message's content array.
// The "type" field is either "text" or "image_url".
type visionContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

// visionMessage is a chat message whose content is an array of parts (multimodal).
type visionMessage struct {
	Role    string              `json:"role"`
	Content []visionContentPart `json:"content"`
}

// textMessage is a plain text-only chat message (used for the system prompt).
type textMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// visionChatRequest is the full request body sent to OpenRouter.
// Messages is []interface{} so we can mix textMessage and visionMessage.
type visionChatRequest struct {
	Model       string        `json:"model"`
	Messages    []interface{} `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type openRouterChatResponse struct {
	Choices []openRouterChoice `json:"choices"`
	Error   *openRouterError   `json:"error,omitempty"`
}

type openRouterChoice struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

type openRouterError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// GradeWithImages – Vision-LLM grading
// ---------------------------------------------------------------------------

// GradeWithImages sends one or more PDF page images (as raw PNG bytes) to the
// configured vision model together with the grading rubric and returns a
// structured GradingFeedback. Each element of pageImages is the raw PNG bytes
// of a single page rendered from the student's PDF answer sheet.
func (c *OpenRouterClient) GradeWithImages(ctx context.Context, rubric string, pageImages [][]byte, maxScore int) (models.GradingFeedback, error) {
	if c.apiKey == "" {
		return models.GradingFeedback{}, fmt.Errorf("openrouter api key is required")
	}
	if len(pageImages) == 0 {
		return models.GradingFeedback{}, fmt.Errorf("no page images provided for grading")
	}

	// Detach from any short-lived caller deadline (e.g. Kafka consumer context)
	// and give the LLM call its own generous but bounded timeout.
	// If the parent context is cancelled (e.g. shutdown) that will still propagate.
	llmCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	ctx = llmCtx

	// Enforce page cap to control cost and stay within token limits.
	if len(pageImages) > MaxPagesPerSubmission {
		pageImages = pageImages[:MaxPagesPerSubmission]
	}

	// System prompt (plain text message).
	systemMsg := textMessage{
		Role: "system",
		Content: "You are a strict academic examiner. " +
			"You will be shown images of a student's handwritten or typed answer sheet. " +
			"Grade the student's answers using the provided rubric and respond with JSON only — " +
			"no markdown fences, no extra commentary.",
	}

	// Build the user message content: text prompt followed by one image per page.
	userParts := []visionContentPart{
		{
			Type: "text",
			Text: fmt.Sprintf(
				"RUBRIC:\n%s\n\nMAX_SCORE: %d\n\n"+
					"The following %d image(s) show the student's answer sheet (one image per page). "+
					"Examine each page carefully and grade the answers.\n\n"+
					"Return ONLY valid JSON with this exact schema:\n"+
					"{\n"+
					"  \"overall_score\": <integer 0-%d>,\n"+
					"  \"summary\": \"<brief overall comment>\",\n"+
					"  \"criteria\": [\n"+
					"    {\"name\": \"<criterion name>\", \"score\": <number>, \"comment\": \"<comment>\"}\n"+
					"  ]\n"+
					"}\n"+
					"No markdown, no extra fields.",
				rubric, maxScore, len(pageImages), maxScore,
			),
		},
	}

	for i, img := range pageImages {
		encoded := base64.StdEncoding.EncodeToString(img)
		userParts = append(userParts, visionContentPart{
			Type: "image_url",
			ImageURL: &imageURL{
				URL: "data:image/jpeg;base64," + encoded,
			},
		})
		_ = i // suppress unused warning
	}

	userMsg := visionMessage{
		Role:    "user",
		Content: userParts,
	}

	payload := visionChatRequest{
		Model:       c.model,
		Messages:    []interface{}{systemMsg, userMsg},
		Temperature: 0.1,
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
		return models.GradingFeedback{}, fmt.Errorf("failed to parse openrouter envelope: %w — body: %s", err, string(respBody))
	}
	if parsed.Error != nil {
		return models.GradingFeedback{}, fmt.Errorf("openrouter api error (code %d): %s", parsed.Error.Code, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return models.GradingFeedback{}, fmt.Errorf("openrouter returned no choices — raw body: %s", string(respBody))
	}

	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	// Strip any accidental markdown code fences the model may have added.
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var feedback models.GradingFeedback
	if err := json.Unmarshal([]byte(content), &feedback); err != nil {
		return models.GradingFeedback{}, fmt.Errorf("failed to parse grading json: %w", err)
	}

	// Clamp score to valid range.
	if feedback.OverallScore < 0 {
		feedback.OverallScore = 0
	}
	if feedback.OverallScore > maxScore {
		feedback.OverallScore = maxScore
	}

	return feedback, nil
}
