package ace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultLLMURL   = "https://api.openai.com/v1/chat/completions"
	defaultLLMModel = "gpt-5-mini"
)

type llmJudge interface {
	Related(ctx context.Context, text, contextText string) (bool, string, error)
}

type openAILLMJudge struct {
	client  *http.Client
	baseURL string
	model   string
}

type questionMatcher struct {
	judge llmJudge
}

func newOpenAILLMJudge(endpoint, model string) *openAILLMJudge {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = defaultLLMURL
	}
	if strings.TrimSpace(model) == "" {
		model = defaultLLMModel
	}
	return &openAILLMJudge{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: endpoint,
		model:   model,
	}
}

func newQuestionMatcher(judge llmJudge) *questionMatcher {
	return &questionMatcher{judge: judge}
}

func (j *openAILLMJudge) Related(ctx context.Context, text, contextText string) (bool, string, error) {
	apiKey := resolveLLMAPIKey()
	if apiKey == "" {
		return false, "", validationErr(fmt.Errorf(
			"LLM filtering isn't available: LLM_API_KEY and OPENAI_API_KEY are not set"))
	}

	body, err := json.Marshal(struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{
		Model: j.model,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{
				Role:    "system",
				Content: "Answer with exactly yes or no.",
			},
			{
				Role:    "user",
				Content: buildRelationPrompt(text, contextText),
			},
		},
	})
	if err != nil {
		return false, "", fmt.Errorf("marshal LLM request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.baseURL, bytes.NewReader(body))
	if err != nil {
		return false, "", fmt.Errorf("create LLM request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := j.client.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("request LLM: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, "", fmt.Errorf("read LLM response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &apiErr); err == nil && apiErr.Error.Message != "" {
			return false, "", fmt.Errorf("LLM API %s: %s", resp.Status, apiErr.Error.Message)
		}
		return false, "", fmt.Errorf("LLM API %s", resp.Status)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return false, "", fmt.Errorf("decode LLM response: %w", err)
	}
	if len(out.Choices) == 0 {
		return false, "", fmt.Errorf("LLM API returned no choices")
	}

	answer := strings.ToLower(strings.TrimSpace(out.Choices[0].Message.Content))
	switch answer {
	case "yes":
		return true, answer, nil
	case "no":
		return false, answer, nil
	default:
		return false, answer, fmt.Errorf("LLM answer was %q, expected yes or no", answer)
	}
}

func resolveLLMAPIKey() string {
	if apiKey := strings.TrimSpace(os.Getenv("LLM_API_KEY")); apiKey != "" {
		return apiKey
	}
	return strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
}

func buildRelationPrompt(text, contextText string) string {
	return fmt.Sprintf(
		"TEXT:\n%s\n\nCTEXT:\n%s\n\nIs TEXT related to CTEXT? Answer either 'yes' or 'no' and nothing else.",
		text,
		contextText,
	)
}

func (m *questionMatcher) matches(ctx context.Context, object map[string]interface{}, predicates []questionPredicate) (bool, error) {
	if len(predicates) == 0 {
		return true, nil
	}
	if m.judge == nil {
		return false, fmt.Errorf("LLM judge is not configured")
	}

	for _, predicate := range predicates {
		values := lookupStringValues(object, predicate.Path)
		if len(values) == 0 {
			return false, nil
		}

		matched := false
		for _, value := range values {
			related, _, err := m.judge.Related(ctx, value, predicate.ContextText)
			if err != nil {
				return false, fmt.Errorf("evaluate LLM predicate at %q: %w", predicate.PathText, err)
			}
			if related {
				matched = true
				break
			}
		}

		if !matched {
			return false, nil
		}
	}

	return true, nil
}
