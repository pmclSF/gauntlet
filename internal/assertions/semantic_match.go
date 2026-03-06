package assertions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// SemanticMatchAssertion runs a semantic match judge in trusted nightly mode.
type SemanticMatchAssertion struct{}

func (a *SemanticMatchAssertion) Type() string { return "semantic_match" }
func (a *SemanticMatchAssertion) IsSoft() bool { return true }

var semanticJudgeHTTPClient = &http.Client{Timeout: 15 * time.Second}

const (
	defaultSemanticJudgeEndpoint = "https://api.openai.com/v1/chat/completions"
)

func (a *SemanticMatchAssertion) Evaluate(ctx Context) Result {
	judgeModel, _ := specString(ctx.Spec, "judge")
	judgeModel = strings.TrimSpace(judgeModel)
	if judgeModel == "" {
		judgeModel = strings.TrimSpace(os.Getenv("GAUNTLET_SEMANTIC_MATCH_MODEL"))
	}
	if judgeModel == "" {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Message:       "semantic_match: missing required field 'judge'",
			DocketHint:    "assertion.spec_invalid",
		}
	}

	prompt, _ := specString(ctx.Spec, "prompt")
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Message:       "semantic_match: missing required field 'prompt'",
			DocketHint:    "assertion.spec_invalid",
		}
	}
	threshold, ok := specFloat(ctx.Spec, "threshold")
	if !ok {
		threshold = 0.8
	}
	if threshold < 0 || threshold > 1 {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Message:       "semantic_match: threshold must be between 0.0 and 1.0",
			DocketHint:    "assertion.spec_invalid",
		}
	}

	mode := strings.ToLower(strings.TrimSpace(ctx.RunnerMode))
	if mode != "nightly" {
		return Result{
			AssertionType: a.Type(),
			Passed:        true,
			Soft:          true,
			Message:       "semantic_match skipped in hermetic mode (runs only in nightly mode)",
		}
	}

	outputText := strings.TrimSpace(string(ctx.Output.Raw))
	if outputText == "" {
		outputText = strings.TrimSpace(fmt.Sprintf("%v", ctx.Output.Parsed))
	}
	if outputText == "" {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Message:       "semantic_match: output is empty",
			DocketHint:    "output.semantic_mismatch",
		}
	}

	score, reason, err := semanticJudgeScore(judgeModel, prompt, outputText)
	if err != nil {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Message:       fmt.Sprintf("semantic_match: judge call failed: %v", err),
			DocketHint:    "assertion.judge_error",
		}
	}
	if score < threshold {
		actual := fmt.Sprintf("semantic score = %.2f", score)
		if strings.TrimSpace(reason) != "" {
			actual = fmt.Sprintf("%s (%s)", actual, strings.TrimSpace(reason))
		}
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Expected:      fmt.Sprintf("semantic score >= %.2f", threshold),
			Actual:        actual,
			Message:       fmt.Sprintf("semantic_match: score %.2f below threshold %.2f", score, threshold),
			DocketHint:    "output.semantic_mismatch",
		}
	}
	message := fmt.Sprintf("semantic_match: score %.2f meets threshold %.2f", score, threshold)
	if strings.TrimSpace(reason) != "" {
		message = fmt.Sprintf("%s (%s)", message, strings.TrimSpace(reason))
	}
	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Soft:          true,
		Message:       message,
	}
}

func semanticJudgeScore(judgeModel, prompt, output string) (float64, string, error) {
	endpoint := strings.TrimSpace(os.Getenv("GAUNTLET_SEMANTIC_MATCH_ENDPOINT"))
	if endpoint == "" {
		endpoint = defaultSemanticJudgeEndpoint
	}
	apiKey := strings.TrimSpace(os.Getenv("GAUNTLET_SEMANTIC_MATCH_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if apiKey == "" {
		return 0, "", fmt.Errorf("missing GAUNTLET_SEMANTIC_MATCH_API_KEY (or OPENAI_API_KEY)")
	}

	reqBody := map[string]interface{}{
		"model":       judgeModel,
		"temperature": 0,
		"messages": []map[string]string{
			{
				"role": "system",
				"content": "Score whether the candidate output satisfies the requirement. " +
					"Return strict JSON with keys: score (0.0-1.0) and reason (short string).",
			},
			{
				"role": "user",
				"content": fmt.Sprintf(
					"Requirement:\n%s\n\nCandidate output:\n%s",
					prompt,
					output,
				),
			},
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return 0, "", fmt.Errorf("marshal judge request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, "", fmt.Errorf("create judge request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := semanticJudgeHTTPClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("send judge request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, "", fmt.Errorf("read judge response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, "", fmt.Errorf("judge endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &completion); err != nil {
		return 0, "", fmt.Errorf("parse judge completion: %w", err)
	}
	if len(completion.Choices) == 0 {
		return 0, "", fmt.Errorf("judge completion returned no choices")
	}
	content := strings.TrimSpace(completion.Choices[0].Message.Content)
	if content == "" {
		return 0, "", fmt.Errorf("judge completion returned empty content")
	}

	var parsed struct {
		Score  interface{} `json:"score"`
		Reason string      `json:"reason"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return 0, "", fmt.Errorf("judge completion content is not valid JSON: %w", err)
	}
	score, err := scoreFromAny(parsed.Score)
	if err != nil {
		return 0, "", fmt.Errorf("judge completion missing valid score: %w", err)
	}
	if score < 0 || score > 1 {
		return 0, "", fmt.Errorf("judge score %.3f out of range [0.0, 1.0]", score)
	}
	return score, strings.TrimSpace(parsed.Reason), nil
}

func scoreFromAny(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported score type %T", v)
	}
}
