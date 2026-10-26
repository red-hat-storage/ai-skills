package skills_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"maragu.dev/gai/eval"
)

const (
	defaultModel     = "gpt-6-luna"
	decisionsModel   = defaultModel
	reasoningEffort  = "medium"
	maxOutputTokens  = 4096
	decisionScoreMax = 2
	responsesURL     = "https://api.openai.com/v1/responses"
	decisionsURL     = "https://api.openai.com/v1/decisions"
)

type inputTokenDetails struct {
	CachedTokens     int64 `json:"cached_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
}

type outputTokenDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

type tokenUsageSummary struct {
	RecordType string   `json:"record_type"`
	Responses  apiUsage `json:"responses"`
	Decisions  apiUsage `json:"decisions"`
}

type apiUsage struct {
	InputTokens         int64              `json:"input_tokens"`
	InputTokensDetails  inputTokenDetails  `json:"input_tokens_details"`
	OutputTokens        int64              `json:"output_tokens"`
	OutputTokensDetails outputTokenDetails `json:"output_tokens_details"`
	TotalTokens         int64              `json:"total_tokens"`
}

func (u *apiUsage) add(other *apiUsage) {
	if other == nil {
		return
	}
	u.InputTokens += other.InputTokens
	u.InputTokensDetails.CachedTokens += other.InputTokensDetails.CachedTokens
	u.InputTokensDetails.CacheWriteTokens += other.InputTokensDetails.CacheWriteTokens
	u.OutputTokens += other.OutputTokens
	u.OutputTokensDetails.ReasoningTokens += other.OutputTokensDetails.ReasoningTokens
	u.TotalTokens += other.TotalTokens
}

type openAIClient struct {
	model         string
	decisionModel string
	apiKey        string
	httpClient    *http.Client
}

func (c openAIClient) postJSON(ctx context.Context, apiName, endpoint string, requestBody any) ([]byte, error) {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s API returned %s: %s", apiName, resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c openAIClient) complete(ctx context.Context, instructions, input string) (string, *apiUsage, error) {
	requestBody := map[string]any{
		"model":             c.model,
		"instructions":      instructions,
		"input":             input,
		"reasoning":         map[string]string{"effort": reasoningEffort},
		"store":             false,
		"max_output_tokens": maxOutputTokens,
	}

	body, err := c.postJSON(ctx, "Responses", responsesURL, requestBody)
	if err != nil {
		return "", nil, err
	}

	var result responseResult
	if err := json.Unmarshal(body, &result); err != nil {
		return "", nil, fmt.Errorf("decode Responses API response: %w", err)
	}
	if result.Status != "" && result.Status != "completed" {
		if result.Error != nil && result.Error.Message != "" {
			return "", result.Usage, fmt.Errorf("Responses API returned status %q: %s", result.Status, result.Error.Message)
		}
		if result.IncompleteDetails != nil && result.IncompleteDetails.Reason != "" {
			return "", result.Usage, fmt.Errorf("Responses API returned status %q: %s", result.Status, result.IncompleteDetails.Reason)
		}
		return "", result.Usage, fmt.Errorf("Responses API returned status %q", result.Status)
	}
	var output strings.Builder
	var outputTypes []string
	for _, item := range result.Output {
		outputTypes = append(outputTypes, item.Type)
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" {
				output.WriteString(content.Text)
			}
		}
	}
	if output.Len() == 0 {
		return "", result.Usage, fmt.Errorf("Responses API returned no text output (status=%q, output item types=%v)", result.Status, outputTypes)
	}
	return output.String(), result.Usage, nil
}

func (c openAIClient) judge(ctx context.Context, question, expected, answer string) (decision, *apiUsage, error) {
	input := fmt.Sprintf("Question:\n%s\n\nReference answer:\n%s\n\nCandidate answer:\n%s", question, expected, answer)
	requestBody := map[string]any{
		"model": c.decisionModel,
		"input": input,
		"questions": []map[string]any{
			{
				"type":         "score",
				"name":         "accuracy",
				"instructions": "How factually accurate is the candidate answer compared with the reference? Judge claims relevant to the question. Do not penalize different wording.",
				"levels": []map[string]string{
					{"label": "materially inaccurate", "description": "Contains major factual errors or contradicts the reference."},
					{"label": "mostly accurate", "description": "Broadly correct with minor factual errors."},
					{"label": "accurate", "description": "Factual claims are correct and consistent with the reference."},
				},
			},
			{
				"type":         "score",
				"name":         "coverage",
				"instructions": "How much of the material information in the reference does the candidate answer cover?",
				"levels": []map[string]string{
					{"label": "low coverage", "description": "Omits most of the important information needed to answer the question."},
					{"label": "partial coverage", "description": "Covers some important information but omits notable points."},
					{"label": "full coverage", "description": "Covers the important information needed to answer the question."},
				},
			},
			{
				"type":         "score",
				"name":         "unsupported_claims",
				"instructions": "How many or how serious are the candidate answer's claims that the reference does not support? A high score means more unsupported claims.",
				"levels": []map[string]string{
					{"label": "none or negligible", "description": "No material unsupported claims."},
					{"label": "some", "description": "One or more minor unsupported claims."},
					{"label": "many or serious", "description": "Multiple unsupported claims or at least one materially misleading claim."},
				},
			},
		},
	}

	body, err := c.postJSON(ctx, "Decisions", decisionsURL, requestBody)
	if err != nil {
		return decision{}, nil, err
	}

	var result decisionsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return decision{}, nil, fmt.Errorf("decode Decisions API response: %w", err)
	}
	grade := decision{}
	seen := make(map[string]bool, len(result.Answers))
	for _, answer := range result.Answers {
		if answer.Type == "refusal" {
			return decision{}, result.Usage, fmt.Errorf("Decisions API refused judge question %q", answer.Name)
		}
		if answer.Type != "score" {
			return decision{}, result.Usage, fmt.Errorf("Decisions API returned unexpected answer type %q for %q", answer.Type, answer.Name)
		}
		if answer.Score < 0 || answer.Score > decisionScoreMax {
			return decision{}, result.Usage, fmt.Errorf("Decisions API score %s is outside [0,2]: %v", answer.Name, answer.Score)
		}
		seen[answer.Name] = true
		switch answer.Name {
		case "accuracy":
			grade.accuracy = eval.Score(answer.Score / decisionScoreMax)
		case "coverage":
			grade.coverage = eval.Score(answer.Score / decisionScoreMax)
		case "unsupported_claims":
			grade.unsupportedClaims = eval.Score(answer.Score / decisionScoreMax)
		default:
			return decision{}, result.Usage, fmt.Errorf("Decisions API returned unexpected answer name %q", answer.Name)
		}
	}
	for _, name := range []string{"accuracy", "coverage", "unsupported_claims"} {
		if !seen[name] {
			return decision{}, result.Usage, fmt.Errorf("Decisions API omitted judge answer %q", name)
		}
	}
	// Decisions API scores range from 0 to 2. gai/eval requires scores from 0 to 1.
	return grade, result.Usage, nil
}

type decisionsResponse struct {
	Usage   *apiUsage `json:"usage"`
	Answers []struct {
		Type  string  `json:"type"`
		Name  string  `json:"name"`
		Score float64 `json:"score"`
	} `json:"answers"`
}

type responseResult struct {
	Usage             *apiUsage `json:"usage"`
	Status            string    `json:"status"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}
