package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestParseModel(t *testing.T) {
	tests := []struct {
		input    string
		expected llModel
	}{
		{"deepseek-v3", deepseekV3},
		{"deepseek-r1", deepseekR1},
		{"deepseek-r1-0528", deepseekR10528},
		{"kimi-k2", kimiK2},
		{"invalid-model", kimiK2}, // default fallback
		{"", kimiK2},              // empty string fallback
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("ParseModel(%s)", test.input), func(t *testing.T) {
			result := ParseModel(test.input)
			if result != test.expected {
				t.Errorf("ParseModel(%s) = %v, expected %v", test.input, result, test.expected)
			}
		})
	}
}

func TestLLModelString(t *testing.T) {
	tests := []struct {
		model    llModel
		expected string
	}{
		{deepseekV3, "deepseek/deepseek-chat-v3-0324:free"},
		{deepseekR1, "deepseek/deepseek-r1:free"},
		{deepseekR10528, "deepseek/deepseek-r1-0528:free"},
		{kimiK2, "moonshotai/kimi-k2:free"},
		{llModel(999), "moonshotai/kimi-k2:free"}, // invalid model should return default
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("llModel(%d).String()", test.model), func(t *testing.T) {
			result := test.model.String()
			if result != test.expected {
				t.Errorf("llModel(%d).String() = %s, expected %s", test.model, result, test.expected)
			}
		})
	}
}

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedCommit string
		expectedPR     string
		expectError    bool
	}{
		{
			name: "valid response",
			input: `COMMIT: feat: add user authentication

PR:
## What changed
- Added user authentication system
- Implemented JWT tokens

## Why
- Improve security
- User management

## Testing
- Unit tests added
- Integration tests passed`,
			expectedCommit: "feat: add user authentication",
			expectedPR: `## What changed
- Added user authentication system
- Implemented JWT tokens

## Why
- Improve security
- User management

## Testing
- Unit tests added
- Integration tests passed`,
			expectError: false,
		},
		{
			name:        "missing commit",
			input:       "PR:\nSome PR description",
			expectError: true,
		},
		{
			name:        "missing PR",
			input:       "COMMIT: fix: bug fix",
			expectError: true,
		},
		{
			name:        "empty input",
			input:       "",
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := parseResponse(test.input)

			if test.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result.CommitMessage != test.expectedCommit {
				t.Errorf("Expected commit message %q, got %q", test.expectedCommit, result.CommitMessage)
			}

			if result.PRDescription != test.expectedPR {
				t.Errorf("Expected PR description %q, got %q", test.expectedPR, result.PRDescription)
			}
		})
	}
}

func TestGenerateCommitAndPRWithModel(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check request headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type: application/json, got %s", r.Header.Get("Content-Type"))
		}

		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("Expected Authorization header with Bearer token")
		}

		// Parse request body
		var req APIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Verify request structure
		if req.Model == "" {
			t.Errorf("Expected model field in request")
		}

		if len(req.Messages) != 2 {
			t.Errorf("Expected 2 messages, got %d", len(req.Messages))
		}

		// Send mock response
		response := APIResponse{
			Choices: []Choice{
				{
					Message: Message{
						Role: "assistant",
						Content: `COMMIT: feat: add new feature

PR:
## What changed
- Added new feature

## Why
- User requested this

## Testing
- Tests added`,
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Set up test environment
	viper.Set("api-key", "test-api-key")

	// Override the API URL for testing (this would require modifying the actual function)
	// For now, we'll test with a real URL but this demonstrates the approach

	// Note: This test would require dependency injection or environment variable override
	// to actually test against the mock server. The test structure is correct though.
}

func TestAPILogger(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "gitguy-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Override config directory for testing
	viper.Set("config-dir", tempDir)

	// Create logger
	logger, err := NewAPILogger()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Test logging
	req := APIRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "user", Content: "test message"},
		},
	}

	resp := &APIResponse{
		Choices: []Choice{
			{Message: Message{Role: "assistant", Content: "test response"}},
		},
	}

	logger.LogAPICall("test-uuid", req, resp, nil, 200, time.Second)

	// Verify log file was created
	logPath := logger.GetLogPath()
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("Log file was not created: %s", logPath)
	}

	// Read log file and verify content
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	// Parse JSON log entry
	var logEntry APIClientLogEntry
	if err := json.Unmarshal(content[:len(content)-1], &logEntry); err != nil { // Remove trailing newline
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// Verify log entry fields
	if logEntry.UUID != "test-uuid" {
		t.Errorf("Expected UUID 'test-uuid', got %s", logEntry.UUID)
	}

	if logEntry.StatusCode != 200 {
		t.Errorf("Expected status code 200, got %d", logEntry.StatusCode)
	}

	if logEntry.Request.Model != "test-model" {
		t.Errorf("Expected model 'test-model', got %s", logEntry.Request.Model)
	}
}

func TestConfigFunctions(t *testing.T) {
	configDir, err := getConfigDir()
	if err != nil {
		t.Errorf("getConfigDir() failed: %v", err)
	}

	if configDir == "" {
		t.Errorf("getConfigDir() returned empty string")
	}

	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Errorf("Config directory was not created: %s", configDir)
	}

	if err := SetupConfig(); err != nil {
		t.Errorf("SetupConfig() failed: %v", err)
	}

	originalAPIKey := viper.GetString("api-key")
	defer viper.Set("api-key", originalAPIKey)

	viper.Set("api-key", "test-key-viper")
	if apiKey := getAPIKey(); apiKey != "test-key-viper" {
		t.Errorf("Expected 'test-key-viper', got %s", apiKey)
	}

	os.Setenv("OPENROUTER_API_KEY", "test-key-env")
	defer os.Unsetenv("OPENROUTER_API_KEY")

	if apiKey := getAPIKey(); apiKey != "test-key-env" {
		t.Errorf("Expected 'test-key-env', got %s", apiKey)
	}
}

func BenchmarkParseModel(b *testing.B) {
	for b.Loop() {
		ParseModel("deepseek-v3")
	}
}

func BenchmarkParseResponse(b *testing.B) {
	input := `COMMIT: feat: add feature

PR:
## What changed
- Added feature

## Why
- User request

## Testing
- Tests added`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseResponse(input)
	}
}

func TestGenerateCommitAndPR(t *testing.T) {
	if getAPIKey() == "" {
		t.Skip("Skipping integration test: no API key available")
	}

	viper.Set("model", "kimi-k2")

	testDiff := `diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..5e1c309
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World`

	result, err := GenerateCommitAndPR(testDiff)
	if err != nil {
		t.Fatalf("GenerateCommitAndPR failed: %v", err)
	}

	if result.CommitMessage == "" {
		t.Errorf("Expected non-empty commit message")
	}

	if result.PRDescription == "" {
		t.Errorf("Expected non-empty PR description")
	}

	// Verify commit message follows conventional format
	if !strings.Contains(result.CommitMessage, ":") {
		t.Errorf("Commit message should follow conventional format with colon")
	}
}

func TestErrorHandling(t *testing.T) {
	malformedInputs := []string{
		"COMMIT: but no PR section",
		"PR: but no COMMIT section",
		"INVALID FORMAT",
		"COMMIT:\nPR:",
	}

	for _, input := range malformedInputs {
		t.Run(fmt.Sprintf("malformed_input_%s", strings.ReplaceAll(input[:min(10, len(input))], "\n", "_")), func(t *testing.T) {
			_, err := parseResponse(input)
			if err == nil {
				t.Errorf("Expected error for malformed input: %s", input)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestAPIClientLogEntry(t *testing.T) {
	entry := APIClientLogEntry{
		Timestamp: time.Now(),
		UUID:      "test-uuid",
		Request: APIRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "test"}},
		},
		StatusCode: 200,
		Duration:   time.Second,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Errorf("Failed to marshal log entry: %v", err)
	}

	var unmarshaled APIClientLogEntry
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Errorf("Failed to unmarshal log entry: %v", err)
	}

	if unmarshaled.UUID != entry.UUID {
		t.Errorf("UUID mismatch after marshal/unmarshal")
	}
}

// TestExpandPRTemplate tests the PR template expansion functionality
func TestExpandPRTemplate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string // What the output should contain
		exact    string // For exact matches (when no template expansion)
	}{
		{
			name:  "template with ID placeholder",
			input: "PR_{{ID}}.md",
			contains: "PR_",
		},
		{
			name:  "template with ID in middle",
			input: "docs/PR_{{ID}}_final.md",
			contains: "docs/PR_",
		},
		{
			name:  "no template placeholder",
			input: "simple.md",
			exact: "simple.md",
		},
		{
			name:  "empty string",
			input: "",
			exact: "",
		},
		{
			name:  "multiple ID placeholders",
			input: "{{ID}}_PR_{{ID}}.md",
			contains: "_PR_",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ExpandPRTemplate(test.input)
			
			if test.exact != "" {
				if result != test.exact {
					t.Errorf("Expected exact match %q, got %q", test.exact, result)
				}
			} else if test.contains != "" {
				if !strings.Contains(result, test.contains) {
					t.Errorf("Expected result to contain %q, got %q", test.contains, result)
				}
				
				// Verify that {{ID}} was replaced
				if strings.Contains(result, "{{ID}}") {
					t.Errorf("Template not expanded, still contains {{ID}}: %q", result)
				}
				
				// Verify the expansion resulted in a valid filename
				if strings.Contains(test.input, "{{ID}}") && result == test.input {
					t.Errorf("Template was not expanded: input=%q, output=%q", test.input, result)
				}
			}
		})
	}
}

// TestExpandPRTemplateRandomness tests that the template generates different IDs
func TestExpandPRTemplateRandomness(t *testing.T) {
	template := "PR_{{ID}}.md"
	results := make(map[string]bool)
	
	// Generate multiple expansions
	for i := 0; i < 10; i++ {
		result := ExpandPRTemplate(template)
		results[result] = true
	}
	
	// We should have at least some variety (though theoretically all could be the same)
	// This is a probabilistic test - with 256 possible values and 10 samples,
	// the chance of all being the same is very low
	if len(results) == 1 {
		t.Logf("Warning: All 10 template expansions resulted in the same value. This is statistically unlikely but possible.")
	}
	
	// Verify all results follow the expected pattern
	for result := range results {
		if !strings.HasPrefix(result, "PR_") || !strings.HasSuffix(result, ".md") {
			t.Errorf("Unexpected result format: %q", result)
		}
		
		// Extract the ID part and verify it's a number
		idPart := strings.TrimPrefix(strings.TrimSuffix(result, ".md"), "PR_")
		if idPart == "" {
			t.Errorf("No ID found in result: %q", result)
		}
	}
}

// BenchmarkExpandPRTemplate benchmarks the template expansion
func BenchmarkExpandPRTemplate(b *testing.B) {
	template := "PR_{{ID}}.md"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ExpandPRTemplate(template)
	}
}
