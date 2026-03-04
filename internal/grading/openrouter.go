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
const DefaultVisionModel = "x-ai/grok-4.1-fast"

// MaxPagesPerSubmission caps how many PDF pages are sent to the LLM per call.
// Keeping this low controls cost and stays within context limits.
const MaxPagesPerSubmission = 10

// MaxRetries is the number of times we retry the LLM call on malformed JSON.
const MaxRetries = 2

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

	forceOrderAgnosticRetry := false
	body, err := c.buildVisionRequestBody(rubric, pageImages, maxScore, forceOrderAgnosticRetry)
	if err != nil {
		return models.GradingFeedback{}, fmt.Errorf("failed to marshal openrouter request: %w", err)
	}

	var feedback models.GradingFeedback
	var lastErr error

	for attempt := 0; attempt <= MaxRetries; attempt++ {
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
			lastErr = fmt.Errorf("openrouter request failed: %w", err)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read openrouter response: %w", err)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("openrouter api returned status %d: %s", resp.StatusCode, string(respBody))
			// Don't retry on 4xx client errors (except 429 rate limit)
			if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
				return models.GradingFeedback{}, lastErr
			}
			continue
		}

		var parsed openRouterChatResponse
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			lastErr = fmt.Errorf("failed to parse openrouter envelope: %w — body: %s", err, string(respBody))
			continue
		}
		if parsed.Error != nil {
			return models.GradingFeedback{}, fmt.Errorf("openrouter api error (code %d): %s", parsed.Error.Code, parsed.Error.Message)
		}
		if len(parsed.Choices) == 0 {
			lastErr = fmt.Errorf("openrouter returned no choices — raw body: %s", string(respBody))
			continue
		}

		content := strings.TrimSpace(parsed.Choices[0].Message.Content)
		// Strip any accidental markdown code fences the model may have added.
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)

		if err := json.Unmarshal([]byte(content), &feedback); err != nil {
			lastErr = fmt.Errorf("failed to parse grading json (attempt %d/%d): %w — raw: %s", attempt+1, MaxRetries+1, err, content)
			continue
		}

		// Validate basic sanity: criteria should not be empty.
		if len(feedback.Criteria) == 0 {
			lastErr = fmt.Errorf("grading returned zero criteria (attempt %d/%d)", attempt+1, MaxRetries+1)
			continue
		}

		if shouldRetryWithOrderAgnosticPrompt(feedback) && !forceOrderAgnosticRetry && attempt < MaxRetries {
			forceOrderAgnosticRetry = true
			body, err = c.buildVisionRequestBody(rubric, pageImages, maxScore, forceOrderAgnosticRetry)
			if err != nil {
				return models.GradingFeedback{}, fmt.Errorf("failed to marshal fallback openrouter request: %w", err)
			}
			lastErr = fmt.Errorf("initial grading looked like false all-zero/unattempted output; retrying with stricter semantic matching prompt")
			continue
		}

		// Success — break out of retry loop.
		lastErr = nil
		break
	}

	if lastErr != nil {
		return models.GradingFeedback{}, lastErr
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

func (c *OpenRouterClient) buildVisionRequestBody(rubric string, pageImages [][]byte, maxScore int, forceOrderAgnostic bool) ([]byte, error) {
	systemPrompt := "You are a meticulous, experienced academic examiner grading a student's answer sheet.\n\n" +
		"GRADING PRINCIPLES:\n" +
		"1. Read every page of the answer sheet carefully before assigning any scores.\n" +
		"2. Grade each question INDEPENDENTLY using only the rubric provided.\n" +
		"3. DO NOT assume the student answered in rubric order. Match each answer to the correct rubric question by topic/content, even if sequence is mixed (e.g., Q3 answered before Q1).\n" +
		"4. If numbering differs or is missing, infer the best rubric-question match from concepts, formulas, terminology, and working steps.\n" +
		"5. Mark a question unattempted only when no relevant answer content exists anywhere in the pages.\n" +
		"6. Award partial credit generously when the student demonstrates understanding, even if the final answer is wrong.\n" +
		"7. Deduct marks only for clear errors, missing steps, or incorrect conclusions — not for handwriting quality or minor formatting issues.\n" +
		"8. If a question is attempted but hard to read, make your best effort to interpret it and note any ambiguity.\n" +
		"9. If the rubric specifies special rules (e.g., 'best 2 of 3', 'mandatory questions', 'internal choice'), apply them precisely.\n" +
		"10. Include ALL attempted questions in the criteria list, even those dropped by rules (mark them with score 0 and explain why).\n" +
		"11. The overall_score MUST equal the sum of scores from all counted criteria (after applying any selection rules).\n" +
		"12. Show your reasoning in calculation_steps so the total is verifiable.\n" +
		"13. Be fair and consistent — do not penalize beyond what the rubric specifies.\n"

	if forceOrderAgnostic {
		systemPrompt += "14. CRITICAL: A fully zero score due only to question-order mismatch is INVALID. Before finalizing, explicitly re-check every rubric criterion against all pages.\n"
	}

	systemPrompt += "\nRESPONSE FORMAT: Respond with valid JSON only — no markdown fences, no commentary outside the JSON."

	userInstructions :=
		"=== GRADING RUBRIC / ANSWER SCHEME ===\n%s\n\n" +
			"=== MAXIMUM POSSIBLE SCORE: %d ===\n\n" +
			"The following %d image(s) show the student's answer sheet (one image per page).\n\n" +
			"INSTRUCTIONS:\n" +
			"1. Carefully read ALL pages before grading.\n" +
			"2. For each rubric question/criterion, find matching student content anywhere in the answer sheet (order can be mixed).\n" +
			"3. Match by semantic/topic similarity, not by positional order on the page.\n" +
			"4. If the student answered in a different sequence (e.g., Q3, then Q1), still map and grade correctly under each rubric criterion.\n" +
			"5. Award marks based on correctness, completeness, and demonstrated understanding.\n" +
			"6. Give partial credit where the student shows correct methodology even if the final answer is wrong.\n" +
			"7. If no relevant content exists for a rubric criterion across all pages, mark it as unattempted with score 0 and explain briefly.\n" +
			"8. Verify that your overall_score equals the sum of the individual criteria scores (after applying any rules).\n"

	if forceOrderAgnostic {
		userInstructions += "9. QUALITY CHECK: if all criteria are 0, re-evaluate once and confirm this is due to genuinely missing content, not numbering/order mismatch.\n"
	}

	userInstructions +=
		"\nReturn ONLY valid JSON with this exact schema:\n" +
			"{\n" +
			"  \"overall_score\": <integer 0-%d>,\n" +
			"  \"summary\": \"<2-3 sentence overall assessment>\",\n" +
			"  \"extracted_rules\": [\"<any special rules found in the rubric>\"],\n" +
			"  \"calculation_steps\": [\"<step-by-step score calculation showing how overall_score was derived>\"],\n" +
			"  \"criteria\": [\n" +
			"    {\n" +
			"      \"name\": \"<question/criterion name>\",\n" +
			"      \"score\": <marks awarded>,\n" +
			"      \"max_score\": <maximum marks for this criterion>,\n" +
			"      \"comment\": \"<specific feedback: what was correct, what was wrong, why marks were deducted>\"\n" +
			"    }\n" +
			"  ]\n" +
			"}\n" +
			"No markdown fences, no extra fields."

	systemMsg := textMessage{Role: "system", Content: systemPrompt}

	userParts := []visionContentPart{{
		Type: "text",
		Text: fmt.Sprintf(userInstructions, rubric, maxScore, len(pageImages), maxScore),
	}}

	for _, img := range pageImages {
		encoded := base64.StdEncoding.EncodeToString(img)
		userParts = append(userParts, visionContentPart{
			Type: "image_url",
			ImageURL: &imageURL{
				URL: "data:image/jpeg;base64," + encoded,
			},
		})
	}

	userMsg := visionMessage{Role: "user", Content: userParts}

	payload := visionChatRequest{
		Model:       c.model,
		Messages:    []interface{}{systemMsg, userMsg},
		Temperature: 0,
	}

	return json.Marshal(payload)
}

func shouldRetryWithOrderAgnosticPrompt(feedback models.GradingFeedback) bool {
	if feedback.OverallScore != 0 || len(feedback.Criteria) == 0 {
		return false
	}

	allZero := true
	hasUnattemptedLanguage := false

	for _, criterion := range feedback.Criteria {
		if criterion.Score != 0 {
			allZero = false
			break
		}

		comment := strings.ToLower(strings.TrimSpace(criterion.Comment))
		if strings.Contains(comment, "not attempted") ||
			strings.Contains(comment, "no attempt") ||
			strings.Contains(comment, "not answered") ||
			strings.Contains(comment, "unanswered") ||
			strings.Contains(comment, "does not correspond") ||
			strings.Contains(comment, "doesn't correspond") {
			hasUnattemptedLanguage = true
		}
	}

	return allZero && hasUnattemptedLanguage
}
