package app

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
)

//go:embed templates/system_prompt.md
var systemPrompt string

type llModel int

const (
	deepseekV3 llModel = iota
	deepseekR10528
	deepseekR1
	kimiK2
)

func (l llModel) String() string {
	switch l {
	case deepseekV3:
		return "deepseek/deepseek-chat-v3-0324:free"
	case deepseekR1:
		return "deepseek/deepseek-r1:free"
	case deepseekR10528:
		return "deepseek/deepseek-r1-0528:free"
	case kimiK2:
		return "moonshotai/kimi-k2:free"
	default:
		return "moonshotai/kimi-k2:free"
	}
}

// ParseModel converts a string model name to an llModel type
func ParseModel(modelName string) llModel {
	switch modelName {
	case "deepseek-v3":
		return deepseekV3
	case "deepseek-r1":
		return deepseekR1
	case "deepseek-r1-0528":
		return deepseekR10528
	case "kimi-k2":
		return kimiK2
	default:
		return kimiK2
	}
}

// APIRequest represents the request payload sent to the OpenRouter API.
type APIRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

// Message represents a single message in the chat history, with a role and content.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// APIResponse represents the response payload received from the OpenRouter API.
type APIResponse struct {
	Choices []Choice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Choice represents a single choice in the OpenRouter response.
type Choice struct {
	Message Message `json:"message"`
}

// LLMResult holds the generated commit message and PR description.
type LLMResult struct {
	CommitMessage string
	PRDescription string
}

// GenerateCommitAndPR sends a git diff to the OpenRouter API and returns a generated
// commit message and PR description as an [LLMResult]
func GenerateCommitAndPR(diff string) (*LLMResult, error) {
	modelName := viper.GetString("model")
	if modelName == "" {
		modelName = "kimi-k2"
	}
	return GenerateCommitAndPRWithModel(diff, ParseModel(modelName))
}

// GenerateCommitAndPRWithModel sends a git diff to the OpenRouter API using a specific model
// and returns a generated commit message and PR description as an [LLMResult]
func GenerateCommitAndPRWithModel(diff string, model llModel) (*LLMResult, error) {
	apiKey := getAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("OpenRouter API key not configured. Set via --api-key flag, OPENROUTER_API_KEY env var, or config file")
	}

	logger, err := NewAPILogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create API logger: %v\n", err)
	}
	defer func() {
		if logger != nil {
			logger.Close()
		}
	}()

	requestUUID := uuid.New().String()

	prTemplateFile := viper.GetString("pr-template")
	if prTemplateFile != "" {
		templateContent, err := os.ReadFile(prTemplateFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read PR template file: %w", err)
		}
		systemPrompt += fmt.Sprintf("\n\nUse this PR template as a guide for the structure and format of the PR description:\n\n%s", string(templateContent))
	}

	userPrompt := fmt.Sprintf("Here is the Git diff to analyze:\n\n```diff\n%s\n```", diff)

	req := APIRequest{
		Model: model.String(),
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("HTTP-Referer", "https://github.com/stormlightlabs/gitguy")
	httpReq.Header.Set("X-Title", "GitGuy")

	client := &http.Client{}
	startTime := time.Now()
	resp, err := client.Do(httpReq)
	duration := time.Since(startTime)

	var openRouterResp *APIResponse
	var statusCode int

	if resp != nil {
		statusCode = resp.StatusCode
		defer resp.Body.Close()
	}

	if err != nil {
		// Log the failed request
		if logger != nil {
			logger.LogAPICall(requestUUID, req, nil, err, statusCode, duration)
		}
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if logger != nil {
			logger.LogAPICall(requestUUID, req, nil, err, statusCode, duration)
		}
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if statusCode != http.StatusOK {
		if logger != nil {
			logger.LogAPICall(requestUUID, req, nil, fmt.Errorf("HTTP %d: %s", statusCode, string(body)), statusCode, duration)
		}
		return nil, fmt.Errorf("API request failed with status %d: %s", statusCode, string(body))
	}

	var parsedResp APIResponse
	if err := json.Unmarshal(body, &parsedResp); err != nil {
		if logger != nil {
			logger.LogAPICall(requestUUID, req, nil, err, statusCode, duration)
		}
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	openRouterResp = &parsedResp

	if openRouterResp.Error != nil {
		if logger != nil {
			logger.LogAPICall(requestUUID, req, openRouterResp, fmt.Errorf("API error: %s", openRouterResp.Error.Message), statusCode, duration)
		}
		return nil, fmt.Errorf("API error: %s", openRouterResp.Error.Message)
	}

	// Log successful request
	if logger != nil {
		logger.LogAPICall(requestUUID, req, openRouterResp, nil, statusCode, duration)
	}

	if len(openRouterResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in API response")
	}

	content := openRouterResp.Choices[0].Message.Content
	return parseResponse(content)
}

// parseResponse parses the raw string response from the LLM into an LLMResult struct.
// It expects the response to be in a specific format with "COMMIT:" and "PR:" prefixes.
func parseResponse(content string) (*LLMResult, error) {
	lines := strings.Split(content, "\n")

	var commitMessage string
	var prLines []string
	inPR := false

	for _, line := range lines {
		if strings.HasPrefix(line, "COMMIT:") {
			commitMessage = strings.TrimSpace(strings.TrimPrefix(line, "COMMIT:"))
		} else if strings.HasPrefix(line, "PR:") {
			inPR = true
			continue
		} else if inPR {
			prLines = append(prLines, line)
		}
	}

	if commitMessage == "" {
		return nil, fmt.Errorf("no commit message found in response")
	}

	prDescription := strings.TrimSpace(strings.Join(prLines, "\n"))
	if prDescription == "" {
		return nil, fmt.Errorf("no PR description found in response")
	}

	return &LLMResult{
			CommitMessage: commitMessage,
			PRDescription: prDescription,
		},
		nil
}
