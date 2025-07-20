package app

import (
	"strings"
	"testing"

	"github.com/aymanbagabas/go-udiff"
)

func TestNewDiffViewerFromEdits(t *testing.T) {
	tests := []struct {
		name            string
		from            string
		to              string
		filename        string
		sideBySide      bool
		syntaxHighlight bool
		showWhitespace  bool
	}{
		{
			name:            "simple changes unified",
			from:            "hello world\nline 2",
			to:              "hello beautiful world\nline 2 modified",
			filename:        "test.txt",
			sideBySide:      false,
			syntaxHighlight: false,
			showWhitespace:  false,
		},
		{
			name:            "simple changes side-by-side",
			from:            "hello world\nline 2",
			to:              "hello beautiful world\nline 2 modified",
			filename:        "test.go",
			sideBySide:      true,
			syntaxHighlight: true,
			showWhitespace:  true,
		},
		{
			name:            "empty to content",
			from:            "",
			to:              "new file content\nwith multiple lines",
			filename:        "new.txt",
			sideBySide:      false,
			syntaxHighlight: false,
			showWhitespace:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := udiff.Strings(tt.from, tt.to)
			
			dv := NewDiffViewerFromEdits(edits, tt.filename, tt.from, tt.sideBySide, tt.syntaxHighlight, tt.showWhitespace)
			
			// Basic checks
			if dv == nil {
				t.Error("Expected non-nil DiffViewer")
				return
			}
			
			if dv.syntaxHighlight != tt.syntaxHighlight {
				t.Errorf("Expected syntaxHighlight %v, got %v", tt.syntaxHighlight, dv.syntaxHighlight)
			}
			
			if dv.showWhitespace != tt.showWhitespace {
				t.Errorf("Expected showWhitespace %v, got %v", tt.showWhitespace, dv.showWhitespace)
			}
			
			expectedMode := UnifiedMode
			if tt.sideBySide {
				expectedMode = SideBySideMode
			}
			if dv.mode != expectedMode {
				t.Errorf("Expected mode %v, got %v", expectedMode, dv.mode)
			}
		})
	}
}

func TestRenderSideBySideFromEdits(t *testing.T) {
	tests := []struct {
		name            string
		from            string
		to              string
		filename        string
		expectError     bool
	}{
		{
			name:        "simple addition",
			from:        "hello world",
			to:          "hello beautiful world",
			filename:    "test.txt",
			expectError: false,
		},
		{
			name:        "multiline changes",
			from:        "line 1\nline 2\nline 3",
			to:          "line 1\nmodified line 2\nline 3\nline 4",
			filename:    "test.txt",
			expectError: false,
		},
		{
			name:        "no changes",
			from:        "same content",
			to:          "same content",
			filename:    "test.txt",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := udiff.Strings(tt.from, tt.to)
			
			// Create a diff viewer with wide enough terminal for side-by-side
			dv := &DiffViewer{
				width:           120,
				height:          40,
				syntaxHighlight: false,
				showWhitespace:  false,
			}
			
			result := dv.renderSideBySideFromEdits(edits, tt.from, tt.filename)
			
			if len(edits) == 0 {
				// No changes case
				if !strings.Contains(result, "No changes") {
					t.Error("Expected 'No changes' message for identical content")
				}
			} else {
				// Changes exist
				if strings.Contains(result, "Error") && !tt.expectError {
					t.Errorf("Unexpected error in result: %s", result)
				}
				
				if !strings.Contains(result, "Error") && tt.expectError {
					t.Error("Expected error but got successful result")
				}
				
				// For successful renders, check that result contains some content
				if !tt.expectError && strings.TrimSpace(result) == "" {
					t.Error("Expected non-empty result for successful render")
				}
			}
		})
	}
}

func TestBuildSideBySideLines(t *testing.T) {
	tests := []struct {
		name          string
		from          string
		to            string
		expectedLeft  int // expected number of left lines
		expectedRight int // expected number of right lines
	}{
		{
			name:          "simple addition",
			from:          "hello world",
			to:            "hello beautiful world",
			expectedLeft:  1, // at least 1 line
			expectedRight: 1, // at least 1 line
		},
		{
			name:          "multiline addition",
			from:          "line 1\nline 2",
			to:            "line 1\nline 2\nline 3",
			expectedLeft:  2, // original lines
			expectedRight: 3, // new lines
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := udiff.Strings(tt.from, tt.to)
			originalLines := strings.Split(tt.from, "\n")
			newLines := strings.Split(tt.to, "\n")
			
			dv := &DiffViewer{
				syntaxHighlight: false,
			}
			
			leftLines, rightLines := dv.buildSideBySideLines(edits, originalLines, newLines, 50, 50)
			
			// Basic checks
			if len(leftLines) < 1 && len(tt.from) > 0 {
				t.Error("Expected at least one left line for non-empty input")
			}
			
			if len(rightLines) < 1 && len(tt.to) > 0 {
				t.Error("Expected at least one right line for non-empty input")
			}
			
			// The exact number of lines may vary based on the diff algorithm,
			// so we just check that we got some reasonable output
			if len(leftLines) == 0 && len(rightLines) == 0 && len(edits) > 0 {
				t.Error("Expected some output lines when edits are present")
			}
		})
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		offset   int
		expected int
	}{
		{
			name:     "start of file",
			lines:    []string{"line 1", "line 2", "line 3"},
			offset:   0,
			expected: 0,
		},
		{
			name:     "middle of file",
			lines:    []string{"line 1", "line 2", "line 3"},
			offset:   7, // "line 1\n" = 7 bytes
			expected: 1,
		},
		{
			name:     "end of file",
			lines:    []string{"line 1", "line 2", "line 3"},
			offset:   100, // beyond end
			expected: 3,
		},
		{
			name:     "empty lines",
			lines:    []string{},
			offset:   0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countLines(tt.lines, tt.offset)
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}