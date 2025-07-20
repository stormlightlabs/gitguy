package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aymanbagabas/go-udiff"
)

func TestGetStagedFileEdits(t *testing.T) {
	tests := []struct {
		name         string
		headContent  string
		stagedContent string
		expectEdits  bool
	}{
		{
			name:         "no changes",
			headContent:  "hello world",
			stagedContent: "hello world",
			expectEdits:  false,
		},
		{
			name:         "simple addition",
			headContent:  "hello world",
			stagedContent: "hello beautiful world",
			expectEdits:  true,
		},
		{
			name:         "simple deletion",
			headContent:  "hello beautiful world",
			stagedContent: "hello world",
			expectEdits:  true,
		},
		{
			name:         "complete replacement",
			headContent:  "old content",
			stagedContent: "new content",
			expectEdits:  true,
		},
		{
			name:         "multiline changes",
			headContent:  "line 1\nline 2\nline 3",
			stagedContent: "line 1\nmodified line 2\nline 3\nline 4",
			expectEdits:  true,
		},
		{
			name:         "empty to content",
			headContent:  "",
			stagedContent: "new file content",
			expectEdits:  true,
		},
		{
			name:         "content to empty",
			headContent:  "file content",
			stagedContent: "",
			expectEdits:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := udiff.Strings(tt.headContent, tt.stagedContent)
			
			hasEdits := len(edits) > 0
			if hasEdits != tt.expectEdits {
				t.Errorf("Expected edits: %v, got edits: %v (count: %d)", tt.expectEdits, hasEdits, len(edits))
			}

			// Verify edits can be applied correctly
			if len(edits) > 0 {
				result, err := udiff.Apply(tt.headContent, edits)
				if err != nil {
					t.Errorf("Failed to apply edits: %v", err)
				}
				if result != tt.stagedContent {
					t.Errorf("Applied result doesn't match expected.\nExpected: %q\nGot: %q", tt.stagedContent, result)
				}
			}
		})
	}
}

func TestFormatEditsAsUnifiedDiff(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		filename string
	}{
		{
			name:     "simple addition",
			from:     "hello world",
			to:       "hello beautiful world",
			filename: "test.txt",
		},
		{
			name:     "simple deletion",
			from:     "hello beautiful world",
			to:       "hello world",
			filename: "example.go",
		},
		{
			name:     "multiline changes",
			from:     "line 1\nline 2\nline 3",
			to:       "line 1\nmodified line 2\nline 3\nline 4",
			filename: "multiline.txt",
		},
		{
			name:     "empty file addition",
			from:     "",
			to:       "new file content\nwith multiple lines",
			filename: "new.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := udiff.Strings(tt.from, tt.to)

			// Use udiff.ToUnified directly instead of the GitRepo method
			oldLabel := "a/" + tt.filename
			newLabel := "b/" + tt.filename
			unifiedDiff, err := udiff.ToUnified(oldLabel, newLabel, tt.from, edits, 3)
			if err != nil {
				t.Errorf("Failed to generate unified diff: %v", err)
				return
			}

			// Basic sanity checks
			if len(edits) > 0 && unifiedDiff == "" {
				t.Error("Expected non-empty unified diff for non-empty edits")
			}

			// Check for basic unified diff format
			if len(edits) > 0 {
				if !containsString(unifiedDiff, "---") {
					t.Error("Unified diff should contain from file marker")
				}
				if !containsString(unifiedDiff, "+++") {
					t.Error("Unified diff should contain to file marker")
				}
				if !containsString(unifiedDiff, tt.filename) {
					t.Error("Unified diff should contain filename")
				}
			}
		})
	}
}

func TestEditsPreservation(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
	}{
		{
			name: "whitespace preservation",
			from: "hello   world\n  indented line",
			to:   "hello\tworld\n    indented line",
		},
		{
			name: "unicode handling",
			from: "hello 世界",
			to:   "hello 世界 🌍",
		},
		{
			name: "empty lines",
			from: "line 1\n\nline 3",
			to:   "line 1\nline 2\n\nline 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := udiff.Strings(tt.from, tt.to)

			// Verify edits can be applied to reproduce the target
			result, err := udiff.Apply(tt.from, edits)
			if err != nil {
				t.Errorf("Failed to apply edits: %v", err)
			}

			if result != tt.to {
				t.Errorf("Edits don't preserve content correctly.\nExpected: %q\nGot: %q", tt.to, result)
			}
		})
	}
}

func TestEditOperations(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		validate func(t *testing.T, edits []udiff.Edit, from, to string)
	}{
		{
			name: "simple change",
			from: "hello world",
			to:   "hello beautiful world",
			validate: func(t *testing.T, edits []udiff.Edit, from, to string) {
				// Just verify that edits are generated and can be applied
				if len(edits) == 0 {
					t.Error("Expected some edits to be generated")
				}
				// Verify the edits can transform from -> to
				result, err := udiff.Apply(from, edits)
				if err != nil {
					t.Errorf("Failed to apply edits: %v", err)
				}
				if result != to {
					t.Errorf("Edits don't produce expected result.\nExpected: %q\nGot: %q", to, result)
				}
			},
		},
		{
			name: "deletion",
			from: "hello beautiful world",
			to:   "hello world",
			validate: func(t *testing.T, edits []udiff.Edit, from, to string) {
				if len(edits) == 0 {
					t.Error("Expected some edits to be generated")
				}
				// Verify the edits can transform from -> to
				result, err := udiff.Apply(from, edits)
				if err != nil {
					t.Errorf("Failed to apply edits: %v", err)
				}
				if result != to {
					t.Errorf("Edits don't produce expected result.\nExpected: %q\nGot: %q", to, result)
				}
			},
		},
		{
			name: "replacement",
			from: "hello world",
			to:   "hello universe",
			validate: func(t *testing.T, edits []udiff.Edit, from, to string) {
				if len(edits) == 0 {
					t.Error("Expected some edits to be generated")
				}
				// Verify the edits can transform from -> to
				result, err := udiff.Apply(from, edits)
				if err != nil {
					t.Errorf("Failed to apply edits: %v", err)
				}
				if result != to {
					t.Errorf("Edits don't produce expected result.\nExpected: %q\nGot: %q", to, result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := udiff.Strings(tt.from, tt.to)
			tt.validate(t, edits, tt.from, tt.to)
		})
	}
}

func TestLargeFileHandling(t *testing.T) {
	// Test with larger content to ensure performance is reasonable
	largeFrom := generateLargeContent(1000, "original")
	largeTo := generateLargeContent(1000, "modified")

	edits := udiff.Strings(largeFrom, largeTo)

	// Verify edits can be applied
	result, err := udiff.Apply(largeFrom, edits)
	if err != nil {
		t.Errorf("Failed to apply edits on large content: %v", err)
	}

	if result != largeTo {
		t.Error("Large content edits don't apply correctly")
	}

	// Basic performance check - shouldn't be excessive number of edits
	if len(edits) > 2000 {
		t.Errorf("Too many edits for large content: %d", len(edits))
	}
}

func TestEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
	}{
		{
			name: "both empty",
			from: "",
			to:   "",
		},
		{
			name: "only newlines",
			from: "\n\n\n",
			to:   "\n\n",
		},
		{
			name: "single character",
			from: "a",
			to:   "b",
		},
		{
			name: "trailing newline addition",
			from: "content",
			to:   "content\n",
		},
		{
			name: "trailing newline removal",
			from: "content\n",
			to:   "content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := udiff.Strings(tt.from, tt.to)

			// Apply and verify
			result, err := udiff.Apply(tt.from, edits)
			if err != nil {
				t.Errorf("Failed to apply edits for edge case: %v", err)
			}

			if result != tt.to {
				t.Errorf("Edge case failed.\nExpected: %q\nGot: %q", tt.to, result)
			}
		})
	}
}

// Helper functions

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && 
		   (needle == "" || indexString(haystack, needle) >= 0)
}

func indexString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func generateLargeContent(lines int, prefix string) string {
	var content []string
	for i := 0; i < lines; i++ {
		content = append(content, fmt.Sprintf("%s line %d with some content", prefix, i))
	}
	return strings.Join(content, "\n")
}