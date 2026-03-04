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
// structured GradingFeedback.
//
// Grading strategy:
//   - Pass 1 (standard): order-agnostic matching via SCAN→MAP→GRADE methodology.
//   - Pass 2 (fallback): triggered automatically if Pass 1 returns suspicious
//     all-zero "not attempted" output that likely indicates an order-mismatch
//     false-negative. Uses a more forceful prompt with explicit re-scan instructions.
func (c *OpenRouterClient) GradeWithImages(ctx context.Context, rubric string, pageImages [][]byte, maxScore int) (models.GradingFeedback, error) {
	if c.apiKey == "" {
		return models.GradingFeedback{}, fmt.Errorf("openrouter api key is required")
	}
	if len(pageImages) == 0 {
		return models.GradingFeedback{}, fmt.Errorf("no page images provided for grading")
	}

	llmCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	ctx = llmCtx

	if len(pageImages) > MaxPagesPerSubmission {
		pageImages = pageImages[:MaxPagesPerSubmission]
	}

	// Pass 1 — standard order-agnostic grading.
	feedback, err := c.runGradingPass(ctx, rubric, pageImages, maxScore, false)
	if err != nil {
		return models.GradingFeedback{}, err
	}

	// Pass 2 — triggered only when Pass 1 looks like a false all-zero result.
	if isSuspiciousAllZero(feedback) {
		fallback, fallbackErr := c.runGradingPass(ctx, rubric, pageImages, maxScore, true)
		if fallbackErr == nil && !isSuspiciousAllZero(fallback) {
			return fallback, nil
		}
		// If fallback also fails or still all-zero, return original Pass 1 result.
	}

	return feedback, nil
}

// runGradingPass executes a single LLM grading call with up to MaxRetries on
// malformed JSON. When forceScan is true a stronger re-scan prompt is used.
func (c *OpenRouterClient) runGradingPass(ctx context.Context, rubric string, pageImages [][]byte, maxScore int, forceScan bool) (models.GradingFeedback, error) {
	messages := c.buildGradingMessages(rubric, pageImages, maxScore, forceScan)

	payload := visionChatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: 0,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return models.GradingFeedback{}, fmt.Errorf("failed to marshal grading request: %w", err)
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
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)

		if err := json.Unmarshal([]byte(content), &feedback); err != nil {
			lastErr = fmt.Errorf("failed to parse grading json (attempt %d/%d): %w — raw: %s", attempt+1, MaxRetries+1, err, content)
			continue
		}

		if len(feedback.Criteria) == 0 {
			lastErr = fmt.Errorf("grading returned zero criteria (attempt %d/%d)", attempt+1, MaxRetries+1)
			continue
		}

		lastErr = nil
		break
	}

	if lastErr != nil {
		return models.GradingFeedback{}, lastErr
	}

	if feedback.OverallScore < 0 {
		feedback.OverallScore = 0
	}
	if feedback.OverallScore > maxScore {
		feedback.OverallScore = maxScore
	}

	return feedback, nil
}

// buildGradingMessages constructs the system + user messages for the LLM.
// When forceScan is true it uses a two-step SCAN→MAP→GRADE prompt and adds
// an explicit re-check directive to prevent false "not attempted" outputs.
func (c *OpenRouterClient) buildGradingMessages(rubric string, pageImages [][]byte, maxScore int, forceScan bool) []interface{} {
	systemContent := strings.Join([]string{
		"You are a strict but fair academic examiner grading a student's answer sheet.",
		"",
		"CORE RULES — follow every rule without exception:",
		"",
		"RULE 1 — READ FIRST, GRADE SECOND.",
		"  Scan every single page completely before assigning any mark.",
		"",
		"RULE 2 — ORDER-AGNOSTIC MATCHING (most important rule).",
		"  The student may have answered questions in ANY order.",
		"  Q1 in the rubric may be answered last on the sheet; Q3 may come first.",
		"  NEVER match by position or sequence. Always match by TOPIC and CONTENT.",
		"  Identify what topic each written section is about, then match it to the",
		"  correct rubric criterion — regardless of the question number written by",
		"  the student or the physical position on the page.",
		"",
		"RULE 3 — SEMANTIC MATCHING.",
		"  If a student writes about 'deadlock', that maps to the Deadlock criterion.",
		"  If a student writes about 'semaphores', check whether the rubric has a",
		"  semaphore criterion; if not, map it to the closest related criterion.",
		"  Concepts, formulas, diagrams, and keywords all count as evidence.",
		"",
		"RULE 4 — PARTIAL CREDIT.",
		"  Award marks whenever the student shows correct understanding of any part,",
		"  even if the final answer or steps are incomplete or wrong.",
		"",
		"RULE 5 — UNATTEMPTED ONLY AS LAST RESORT.",
		"  Only mark a criterion as unattempted (score 0) when you have confirmed",
		"  there is absolutely zero relevant content anywhere across ALL pages.",
		"  If you find even partial relevant content, award partial marks.",
		"",
		"RULE 6 — SCORE CONSISTENCY.",
		"  overall_score MUST equal the arithmetic sum of all counted criteria scores.",
		"  Show your calculation in calculation_steps.",
		"",
		"OUTPUT: Respond with valid JSON only. No markdown fences, no extra text.",
	}, "\n")

	var userText string
	if forceScan {
		userText = fmt.Sprintf(
			"=== GRADING RUBRIC / ANSWER SCHEME ===\n%s\n\n"+
				"=== MAXIMUM POSSIBLE SCORE: %d ===\n\n"+
				"The following %d image(s) show the student's answer sheet.\n\n"+
				"MANDATORY TWO-STEP PROCESS — do both steps before writing the JSON:\n\n"+
				"STEP 1 — CONTENT SCAN (do this mentally before grading):\n"+
				"  For each page, list every distinct section/paragraph the student wrote.\n"+
				"  Note the topic/subject of each section (e.g. scheduling, deadlock, IPC...).\n"+
				"  Ignore numbering written by the student — focus only on the actual topic.\n\n"+
				"STEP 2 — MAP AND GRADE:\n"+
				"  For each rubric criterion, find the student section from Step 1 whose\n"+
				"  topic best matches that criterion. Grade based on correctness/completeness.\n"+
				"  A different question number ≠ unattempted. Topic mismatch is the only\n"+
				"  valid reason to mark something as unattempted.\n\n"+
				"IMPORTANT: If a previous grading attempt returned all zeros, that was WRONG.\n"+
				"  Re-scan all pages now. If the sheet has written content, it must be graded.\n\n"+
				"Return ONLY valid JSON matching this schema exactly:\n"+
				"{\n"+
				"  \"overall_score\": <integer 0-%d>,\n"+
				"  \"summary\": \"<2-3 sentence overall assessment>\",\n"+
				"  \"extracted_rules\": [\"<special rubric rules found, if any>\"],\n"+
				"  \"calculation_steps\": [\"<step-by-step score derivation>\"],\n"+
				"  \"criteria\": [\n"+
				"    {\n"+
				"      \"name\": \"<rubric criterion name>\",\n"+
				"      \"score\": <marks awarded>,\n"+
				"      \"max_score\": <max marks for this criterion>,\n"+
				"      \"comment\": \"<what matched, what was correct/incorrect, why marks given/deducted>\"\n"+
				"    }\n"+
				"  ]\n"+
				"}\n"+
				"No markdown fences, no extra fields.",
			rubric, maxScore, len(pageImages), maxScore,
		)
	} else {
		userText = fmt.Sprintf(
			"=== GRADING RUBRIC / ANSWER SCHEME ===\n%s\n\n"+
				"=== MAXIMUM POSSIBLE SCORE: %d ===\n\n"+
				"The following %d image(s) show the student's answer sheet.\n\n"+
				"GRADING PROCESS:\n"+
				"1. Read ALL pages completely before assigning any score.\n"+
				"2. For each rubric criterion, search the ENTIRE answer sheet for relevant content.\n"+
				"   The student may have answered in a different order than the rubric —\n"+
				"   match by TOPIC and CONTENT, never by position or student-written question number.\n"+
				"3. If the student answered Q3 first and Q1 last, map each section to the\n"+
				"   correct rubric criterion by what the section is actually about.\n"+
				"4. Award partial credit wherever the student shows understanding of any sub-part.\n"+
				"5. Mark as unattempted ONLY if zero relevant content exists anywhere across all pages.\n"+
				"6. overall_score = sum of all counted criteria scores (show working in calculation_steps).\n\n"+
				"Return ONLY valid JSON matching this schema exactly:\n"+
				"{\n"+
				"  \"overall_score\": <integer 0-%d>,\n"+
				"  \"summary\": \"<2-3 sentence overall assessment>\",\n"+
				"  \"extracted_rules\": [\"<special rubric rules found, if any>\"],\n"+
				"  \"calculation_steps\": [\"<step-by-step score derivation>\"],\n"+
				"  \"criteria\": [\n"+
				"    {\n"+
				"      \"name\": \"<rubric criterion name>\",\n"+
				"      \"score\": <marks awarded>,\n"+
				"      \"max_score\": <max marks for this criterion>,\n"+
				"      \"comment\": \"<what matched, what was correct/incorrect, why marks given/deducted>\"\n"+
				"    }\n"+
				"  ]\n"+
				"}\n"+
				"No markdown fences, no extra fields.",
			rubric, maxScore, len(pageImages), maxScore,
		)
	}

	userParts := []visionContentPart{{Type: "text", Text: userText}}

	for _, img := range pageImages {
		encoded := base64.StdEncoding.EncodeToString(img)
		userParts = append(userParts, visionContentPart{
			Type:     "image_url",
			ImageURL: &imageURL{URL: "data:image/jpeg;base64," + encoded},
		})
	}

	return []interface{}{
		textMessage{Role: "system", Content: systemContent},
		visionMessage{Role: "user", Content: userParts},
	}
}

// isSuspiciousAllZero returns true when every criterion scored 0 AND at least
// one comment contains language that suggests an order/numbering mismatch rather
// than genuinely unattempted work. This is used to trigger a fallback grading pass.
func isSuspiciousAllZero(f models.GradingFeedback) bool {
	if f.OverallScore != 0 || len(f.Criteria) == 0 {
		return false
	}
	for _, c := range f.Criteria {
		if c.Score != 0 {
			return false
		}
	}
	// All criteria scored 0 — check if comments hint at a mismatch false-negative.
	mismatchPhrases := []string{
		"not attempted", "no attempt", "not answered", "unanswered",
		"does not correspond", "doesn't correspond", "not related",
		"unrelated", "wrong question", "different question",
		"not matching", "no relevant", "does not match",
	}
	for _, c := range f.Criteria {
		lower := strings.ToLower(c.Comment)
		for _, phrase := range mismatchPhrases {
			if strings.Contains(lower, phrase) {
				return true
			}
		}
	}
	return false
}
