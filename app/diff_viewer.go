package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/formatters"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/muesli/reflow/ansi"
	"github.com/muesli/reflow/truncate"
	"github.com/muesli/reflow/wordwrap"
)

var sink, _ = os.Create(uuid.New().String() + ".log")
var logger = log.NewWithOptions(sink, log.Options{Level: log.DebugLevel})

// ViewMode represents the current viewing mode
type ViewMode int

const (
	UnifiedMode ViewMode = iota
	SideBySideMode
)

type Op = udiff.OpKind

// DiffLine represents a single line in the diff with rendering information
type DiffLine struct {
	Content     string
	Operation   string // "add", "delete", "context"
	LineNumber  int
	DisplayText string
	Height      int // Visual height for wrapped lines
}

// LineMapping maps corresponding lines between left and right panes
type LineMapping struct {
	// Index in leftLines (-1 if no corresponding line)
	LeftIndex int
	// Index in rightLines (-1 if no corresponding line)
	RightIndex int
	// Logical line number for synchronization
	LogicalLine int
}

// DiffViewer represents the diff viewer model
type DiffViewer struct {
	leftViewport    viewport.Model
	rightViewport   viewport.Model
	unifiedViewport viewport.Model

	content         string
	syntaxHighlight bool
	showWhitespace  bool
	width           int
	height          int
	err             error

	leftLines   []DiffLine
	rightLines  []DiffLine
	lineMapping []LineMapping

	// Current state
	mode       ViewMode
	scrollSync bool
}

// NewDiffViewerFromEdits creates a new diff viewer from udiff.Edit operations
func NewDiffViewerFromEdits(edits []udiff.Edit, filename, originalContent string, sideBySide, syntaxHighlight, showWhitespace bool) *DiffViewer {
	oldLabel := "a/" + filename
	newLabel := "b/" + filename
	content, err := udiff.ToUnified(oldLabel, newLabel, originalContent, edits, 3)
	if err != nil {
		// Fallback to empty content if conversion fails
		content = ""
	}
	return NewDiffViewer(content, sideBySide, syntaxHighlight, showWhitespace)
}

// NewDiffViewer creates a new diff viewer with the given content and options
func NewDiffViewer(content string, sideBySide, syntaxHighlight, showWhitespace bool) *DiffViewer {
	leftVp := viewport.New(0, 0)
	rightVp := viewport.New(0, 0)
	unifiedVp := viewport.New(0, 0)

	mode := UnifiedMode
	if sideBySide {
		mode = SideBySideMode
	}

	dv := &DiffViewer{
		leftViewport:    leftVp,
		rightViewport:   rightVp,
		unifiedViewport: unifiedVp,
		content:         content,
		syntaxHighlight: syntaxHighlight,
		showWhitespace:  showWhitespace,
		mode:            mode,
		scrollSync:      true, // Enable by default
	}

	return dv
}

// Init initializes the diff viewer
func (dv *DiffViewer) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the diff viewer
func (dv *DiffViewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		dv.width = msg.Width
		dv.height = msg.Height

		// Update viewport dimensions based on current mode
		if dv.mode == SideBySideMode {
			leftWidth, rightWidth := dv.calculatePaneWidths()
			dv.leftViewport.Width = leftWidth
			dv.rightViewport.Width = rightWidth
			dv.leftViewport.Height = dv.height - 6 // Reserve space for title and help
			dv.rightViewport.Height = dv.height - 6
		} else {
			dv.unifiedViewport.Width = dv.width - 4
			dv.unifiedViewport.Height = dv.height - 6
		}

		// Re-render the diff content with new dimensions
		dv.renderDiff()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return dv, tea.Quit
		case "s":
			// Toggle side-by-side mode
			if dv.mode == SideBySideMode {
				dv.mode = UnifiedMode
			} else {
				dv.mode = SideBySideMode
			}
			dv.renderDiff()
		case "h":
			// Toggle syntax highlighting
			dv.syntaxHighlight = !dv.syntaxHighlight
			dv.renderDiff()
		case "y":
			// Toggle scroll synchronization (only in side-by-side mode)
			if dv.mode == SideBySideMode {
				dv.scrollSync = !dv.scrollSync
			}
		case "w":
			// Toggle whitespace display
			dv.showWhitespace = !dv.showWhitespace
			dv.renderDiff()
		}
	}

	// Update the appropriate viewport based on current mode
	if dv.mode == SideBySideMode {
		var leftCmd, rightCmd tea.Cmd

		// Store previous scroll positions for sync detection
		prevLeftY := dv.leftViewport.YOffset
		prevRightY := dv.rightViewport.YOffset

		dv.leftViewport, leftCmd = dv.leftViewport.Update(msg)
		dv.rightViewport, rightCmd = dv.rightViewport.Update(msg)

		// Implement synchronized scrolling if enabled
		if dv.scrollSync {
			// Check if left viewport scrolled and sync right viewport
			if dv.leftViewport.YOffset != prevLeftY {
				dv.rightViewport.YOffset = dv.leftViewport.YOffset
			}
			// Check if right viewport scrolled and sync left viewport
			if dv.rightViewport.YOffset != prevRightY {
				dv.leftViewport.YOffset = dv.rightViewport.YOffset
			}
		}

		return dv, tea.Batch(leftCmd, rightCmd)
	} else {
		dv.unifiedViewport, cmd = dv.unifiedViewport.Update(msg)
		return dv, cmd
	}
}

// View renders the diff viewer
func (dv *DiffViewer) View() string {
	if dv.err != nil {
		return fmt.Sprintf("Error parsing diff: %v\n\nPress q to quit.", dv.err)
	}

	var b strings.Builder

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Render("Git Diff Viewer")

	actualMode := "unified"
	requestedMode := "unified"
	if dv.mode == SideBySideMode {
		requestedMode = "side-by-side"
		useSideBySide, _, _ := dv.calculateLayout()
		if useSideBySide {
			actualMode = "side-by-side"
		} else {
			actualMode = "unified (auto)"
		}
	}

	modeInfo := fmt.Sprintf("Mode: %s", actualMode)
	if requestedMode != actualMode {
		modeInfo += fmt.Sprintf(" (requested: %s)", requestedMode)
	}

	// Add scroll sync status for side-by-side mode
	syncInfo := ""
	if dv.mode == SideBySideMode {
		syncInfo = fmt.Sprintf(" | Scroll sync: %t", dv.scrollSync)
	}

	subtitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(fmt.Sprintf("%s | Syntax highlighting: %t | Whitespace: %t%s | Width: %d", modeInfo, dv.syntaxHighlight, dv.showWhitespace, syncInfo, dv.width))

	b.WriteString(title + " " + subtitle + "\n\n")

	// Content
	if dv.mode == SideBySideMode {
		leftContent := dv.leftViewport.View()
		rightContent := dv.rightViewport.View()

		// Split into lines
		leftLines := strings.Split(leftContent, "\n")
		rightLines := strings.Split(rightContent, "\n")

		// Remove empty trailing lines
		for len(leftLines) > 0 && strings.TrimSpace(leftLines[len(leftLines)-1]) == "" {
			leftLines = leftLines[:len(leftLines)-1]
		}
		for len(rightLines) > 0 && strings.TrimSpace(rightLines[len(rightLines)-1]) == "" {
			rightLines = rightLines[:len(rightLines)-1]
		}

		// Display left and right panes side by side with proper width management
		leftWidth, rightWidth := dv.calculatePaneWidths()
		maxLines := len(leftLines)
		if len(rightLines) > maxLines {
			maxLines = len(rightLines)
		}

		for i := 0; i < maxLines; i++ {
			left := ""
			right := ""
			if i < len(leftLines) {
				left = leftLines[i]
			}
			if i < len(rightLines) {
				right = rightLines[i]
			}

			// Ensure exact width for each pane
			leftFormatted := dv.fitToWidth(left, leftWidth)
			rightFormatted := dv.fitToWidth(right, rightWidth)

			// Combine with separator
			line := leftFormatted + " │ " + rightFormatted
			b.WriteString(line + "\n")
		}
	} else {
		b.WriteString(dv.unifiedViewport.View())
	}

	// Help
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	help := helpStyle.Render("j/k: scroll | s: toggle side-by-side | h: toggle syntax highlighting | w: toggle whitespace | y: toggle scroll sync | q: quit")

	// Add responsive layout info for narrow terminals
	if dv.width < 100 {
		responsiveInfo := helpStyle.Render("Terminal width: " + fmt.Sprintf("%d", dv.width) + " (side-by-side requires ≥100)")
		help = help + "\n" + responsiveInfo
	}

	b.WriteString("\n\n" + help)

	return b.String()
}

func (dv *DiffViewer) calculatePaneWidths() (leftWidth, rightWidth int) {
	// Use only 90% of available width to prevent reflow issues
	maxUsableWidth := int(float64(dv.width) * 0.90)
	available := maxUsableWidth - 3 // Reserve for separator " │ "
	leftWidth = available / 2
	rightWidth = available - leftWidth
	return leftWidth, rightWidth
}

func (dv *DiffViewer) renderDiff() {
	if dv.err != nil {
		errorContent := "Error parsing diff: " + dv.err.Error()
		dv.unifiedViewport.SetContent(errorContent)
		dv.leftViewport.SetContent(errorContent)
		dv.rightViewport.SetContent(errorContent)
		return
	}

	if dv.content == "" {
		noContent := "No diff content to display"
		dv.unifiedViewport.SetContent(noContent)
		dv.leftViewport.SetContent(noContent)
		dv.rightViewport.SetContent(noContent)
		return
	}

	if dv.mode == SideBySideMode {
		dv.renderSideBySidePanes()
	} else {
		// Render unified view - just display the content as-is
		content := dv.content
		if dv.syntaxHighlight {
			content = dv.applySyntaxHighlighting(content, "diff")
		}
		dv.unifiedViewport.SetContent(content)
	}
}

func (dv *DiffViewer) renderSideBySidePanes() {
	leftWidth, rightWidth := dv.calculatePaneWidths()

	// Parse the unified diff content for better side-by-side rendering
	lines := strings.Split(dv.content, "\n")

	var leftLines, rightLines []string
	leftLineNum := 1
	rightLineNum := 1

	// Parse header info for line numbers
	var oldStart, newStart int

	// Group consecutive deletions and additions for alignment
	i := 0
	for i < len(lines) {
		line := lines[i]

		// Skip empty lines early to prevent bounds issues
		if line == "" {
			i++
			continue
		}

		// Parse hunk headers to get starting line numbers
		if strings.HasPrefix(line, "@@") {
			// Extract line numbers from hunk header: @@ -oldStart,oldCount +newStart,newCount @@
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				// Parse -oldStart
				if strings.HasPrefix(parts[1], "-") {
					fmt.Sscanf(parts[1], "-%d", &oldStart)
					leftLineNum = oldStart
				}
				// Parse +newStart
				if strings.HasPrefix(parts[2], "+") {
					fmt.Sscanf(parts[2], "+%d", &newStart)
					rightLineNum = newStart
				}
			}
			// Headers go to both sides without line numbers
			headerStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("6")).
				Bold(true)
			styledLine := headerStyle.Render(line)
			leftLines = append(leftLines, dv.fitToWidth(styledLine, leftWidth))
			rightLines = append(rightLines, dv.fitToWidth(styledLine, rightWidth))
			i++
			continue
		}

		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") ||
			strings.HasPrefix(line, "diff --git") {
			// File headers go to both sides without line numbers
			headerStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("33")).
				Bold(true)
			styledLine := headerStyle.Render(line)
			leftLines = append(leftLines, dv.fitToWidth(styledLine, leftWidth))
			rightLines = append(rightLines, dv.fitToWidth(styledLine, rightWidth))
			i++
			continue
		}

		if strings.HasPrefix(line, "-") {
			// Collect all consecutive deletions
			var deletions []string
			j := i
			for j < len(lines) && strings.HasPrefix(lines[j], "-") {
				deletions = append(deletions, lines[j])
				j++
			}

			// Collect all consecutive additions that follow
			var additions []string
			for j < len(lines) && strings.HasPrefix(lines[j], "+") {
				additions = append(additions, lines[j])
				j++
			}

			// Align deletions and additions
			// maxChanges := len(deletions)
			// if len(additions) > maxChanges {
			// 	maxChanges = len(additions)
			// }

			// Process deletions and additions separately to handle wrapping properly
			var leftWrappedLines, rightWrappedLines []string

			// Handle all deletions
			for _, deletion := range deletions {
				content := ""
				if len(deletion) > 1 {
					content = deletion[1:] // Remove the '-' prefix
				}

				// Skip whitespace-only changes if showWhitespace is false
				// But preserve genuinely empty lines (which have content == "")
				if !dv.showWhitespace && content != "" && strings.TrimSpace(content) == "" {
					leftLineNum++
					continue
				}

				// Apply syntax highlighting if enabled
				if dv.syntaxHighlight {
					content = dv.applySyntaxHighlighting(content, "go")
				}

				deleteLineNumStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("196")).
					Background(lipgloss.Color("52"))
				deleteBgStyle := lipgloss.NewStyle().
					Background(lipgloss.Color("52"))

				// Use wrapContentWithStyle for proper wrapping
				wrappedLines := dv.wrapContentWithStyle(leftLineNum, " │ -", content, deleteLineNumStyle, deleteBgStyle, leftWidth)
				leftWrappedLines = append(leftWrappedLines, wrappedLines...)
				leftLineNum++
			}

			// Handle all additions
			for _, addition := range additions {
				content := ""
				if len(addition) > 1 {
					content = addition[1:] // Remove the '+' prefix
				}

				// Skip whitespace-only changes if showWhitespace is false
				// But preserve genuinely empty lines (which have content == "")
				if !dv.showWhitespace && content != "" && strings.TrimSpace(content) == "" {
					rightLineNum++
					continue
				}

				// Apply syntax highlighting if enabled
				if dv.syntaxHighlight {
					content = dv.applySyntaxHighlighting(content, "go")
				}

				addLineNumStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("46")).
					Background(lipgloss.Color("22"))
				addBgStyle := lipgloss.NewStyle().
					Background(lipgloss.Color("22"))

				// Use wrapContentWithStyle for proper wrapping
				wrappedLines := dv.wrapContentWithStyle(rightLineNum, " │ +", content, addLineNumStyle, addBgStyle, rightWidth)
				rightWrappedLines = append(rightWrappedLines, wrappedLines...)
				rightLineNum++
			}

			// Balance wrapped lines and add them to main arrays
			maxWrappedLines := len(leftWrappedLines)
			if len(rightWrappedLines) > maxWrappedLines {
				maxWrappedLines = len(rightWrappedLines)
			}

			// Pad shorter side with empty lines
			for len(leftWrappedLines) < maxWrappedLines {
				leftWrappedLines = append(leftWrappedLines, dv.fitToWidth("", leftWidth))
			}
			for len(rightWrappedLines) < maxWrappedLines {
				rightWrappedLines = append(rightWrappedLines, dv.fitToWidth("", rightWidth))
			}

			// Add all balanced lines
			leftLines = append(leftLines, leftWrappedLines...)
			rightLines = append(rightLines, rightWrappedLines...)

			i = j // Move to next unprocessed line

		} else if strings.HasPrefix(line, " ") {
			// Context lines go to both sides
			content := ""
			if len(line) > 1 {
				content = line[1:] // Remove the ' ' prefix
			}

			// Apply syntax highlighting if enabled
			if dv.syntaxHighlight {
				content = dv.applySyntaxHighlighting(content, "go")
			}

			// Style for context lines
			contextLineNumStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))
			contextBgStyle := lipgloss.NewStyle() // No special background for context

			// Use wrapContentWithStyle for both sides
			leftWrappedLines := dv.wrapContentWithStyle(leftLineNum, " │  ", content, contextLineNumStyle, contextBgStyle, leftWidth)
			rightWrappedLines := dv.wrapContentWithStyle(rightLineNum, " │  ", content, contextLineNumStyle, contextBgStyle, rightWidth)

			// Balance wrapped lines for context (should be same content, so same number of lines)
			maxWrappedLines := len(leftWrappedLines)
			if len(rightWrappedLines) > maxWrappedLines {
				maxWrappedLines = len(rightWrappedLines)
			}

			// Pad shorter side with empty lines (shouldn't be needed for context, but safety)
			for len(leftWrappedLines) < maxWrappedLines {
				leftWrappedLines = append(leftWrappedLines, dv.fitToWidth("", leftWidth))
			}
			for len(rightWrappedLines) < maxWrappedLines {
				rightWrappedLines = append(rightWrappedLines, dv.fitToWidth("", rightWidth))
			}

			// Add all wrapped lines
			leftLines = append(leftLines, leftWrappedLines...)
			rightLines = append(rightLines, rightWrappedLines...)

			leftLineNum++
			rightLineNum++
			i++
		} else {
			// Unknown line type, skip
			i++
		}
	}

	// Balance the lines
	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	for len(leftLines) < maxLines {
		leftLines = append(leftLines, dv.fitToWidth("", leftWidth))
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, dv.fitToWidth("", rightWidth))
	}

	dv.leftViewport.SetContent(strings.Join(leftLines, "\n"))
	dv.rightViewport.SetContent(strings.Join(rightLines, "\n"))
}

func (dv *DiffViewer) applySyntaxHighlighting(content, filename string) string {
	if !dv.syntaxHighlight {
		return content
	}

	// Get lexer based on filename
	lexer := lexers.Match(filename)
	if lexer == nil {
		ext := filepath.Ext(filename)
		if len(ext) > 1 {
			lexer = lexers.Get(ext[1:]) // Remove the dot
		}
	}
	if lexer == nil {
		return content // No highlighting available
	}

	// Use a style
	style := styles.Get("github")
	if style == nil {
		style = styles.Fallback
	}

	// Use terminal formatter
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		return content
	}

	// Apply highlighting
	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return content
	}

	var highlighted strings.Builder
	err = formatter.Format(&highlighted, style, iterator)
	if err != nil {
		return content
	}

	return highlighted.String()
}

func (dv *DiffViewer) calculateLayout() (bool, int, int) {
	const (
		minTerminalWidth   = 60
		minSideBySideWidth = 100
		dividerWidth       = 3 // " │ "
		margins            = 4 // Account for viewport margins
	)

	// Use only 90% of available width to prevent reflow issues
	maxUsableWidth := int(float64(dv.width) * 0.90)
	availableWidth := maxUsableWidth - margins

	// Force unified mode for narrow terminals
	if availableWidth < minSideBySideWidth {
		return false, availableWidth, 0
	}

	// Calculate side-by-side layout
	contentWidth := availableWidth - dividerWidth
	leftWidth := contentWidth / 2
	rightWidth := contentWidth - leftWidth

	// Ensure minimum column width
	minColumnWidth := 40
	if leftWidth < minColumnWidth || rightWidth < minColumnWidth {
		return false, availableWidth, 0
	}

	return true, leftWidth, rightWidth
}

// renderSideBySideFromEdits renders side-by-side diff directly from udiff.Edit operations
func (dv *DiffViewer) renderSideBySideFromEdits(edits []udiff.Edit, originalContent, filename string) string {
	if len(edits) == 0 {
		return "No changes to display"
	}

	// Check if we should use side-by-side layout based on terminal width
	useSideBySide, leftWidth, rightWidth := dv.calculateLayout()
	if !useSideBySide {
		// Terminal too narrow, fall back to unified mode
		unifiedDiff, err := udiff.ToUnified("a/"+filename, "b/"+filename, originalContent, edits, 3)
		if err != nil {
			return "Error generating diff"
		}
		return unifiedDiff
	}

	// Apply edits to get the new content
	newContent, err := udiff.Apply(originalContent, edits)
	if err != nil {
		return "Error applying edits"
	}

	// Split into lines for side-by-side rendering
	originalLines := strings.Split(originalContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Build side-by-side representation
	leftLines, rightLines := dv.buildSideBySideLines(edits, originalLines, newLines, leftWidth, rightWidth)

	// Balance the lines
	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	for len(leftLines) < maxLines {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, "")
	}

	var b strings.Builder
	for i := 0; i < maxLines; i++ {
		left := leftLines[i]
		right := rightLines[i]

		// Ensure exact visual width
		leftVisual := ansi.PrintableRuneWidth(left)
		rightVisual := ansi.PrintableRuneWidth(right)

		// Truncate if too long
		if leftVisual > leftWidth {
			left = truncate.String(left, uint(leftWidth))
			leftVisual = leftWidth
		}
		if rightVisual > rightWidth {
			right = truncate.String(right, uint(rightWidth))
			rightVisual = rightWidth
		}

		// Manual padding to ensure exact width
		if leftVisual < leftWidth {
			left = left + strings.Repeat(" ", leftWidth-leftVisual)
		}
		if rightVisual < rightWidth {
			right = right + strings.Repeat(" ", rightWidth-rightVisual)
		}

		line := left + " │ " + right
		b.WriteString(line + "\n")
	}

	return b.String()
}

// buildSideBySideLines converts Edit operations to left/right line representations
func (dv *DiffViewer) buildSideBySideLines(edits []udiff.Edit, originalLines, _ []string, _, _ int) ([]string, []string) {
	var leftLines, rightLines []string

	// This is a simplified implementation that treats each edit independently
	// A more sophisticated version would handle overlapping edits and line alignment
	originalOffset := 0
	newOffset := 0

	for _, edit := range edits {
		// Calculate line positions
		originalStart := countLines(originalLines, edit.Start)
		editOriginalLines := countLines(originalLines[originalStart:], edit.End-edit.Start)
		editNewLines := strings.Count(edit.New, "\n")

		// Add unchanged lines before this edit
		for originalOffset < originalStart {
			if originalOffset < len(originalLines) {
				line := originalLines[originalOffset]
				if dv.syntaxHighlight {
					line = dv.applySyntaxHighlighting(line, getFileExtension(edit.Start))
				}
				leftLines = append(leftLines, " "+line)
				rightLines = append(rightLines, " "+line)
			}
			originalOffset++
			newOffset++
		}

		// Add deleted lines (left side only)
		for i := range editOriginalLines {
			if originalOffset+i < len(originalLines) {
				line := originalLines[originalOffset+i]
				deleteStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("196")).
					Background(lipgloss.Color("52"))
				leftLines = append(leftLines, deleteStyle.Render("-"+line))
				rightLines = append(rightLines, "")
			}
		}

		// Add new lines (right side only)
		if edit.New != "" {
			newContentLines := strings.Split(edit.New, "\n")
			for _, line := range newContentLines {
				if line != "" || len(newContentLines) > 1 {
					addStyle := lipgloss.NewStyle().
						Foreground(lipgloss.Color("46")).
						Background(lipgloss.Color("22"))
					leftLines = append(leftLines, "")
					rightLines = append(rightLines, addStyle.Render("+"+line))
				}
			}
		}

		originalOffset += editOriginalLines
		newOffset += editNewLines
	}

	// Add remaining unchanged lines
	for originalOffset < len(originalLines) {
		line := originalLines[originalOffset]
		leftLines = append(leftLines, " "+line)
		rightLines = append(rightLines, " "+line)
		originalOffset++
	}

	return leftLines, rightLines
}

// Helper function to count lines up to a byte offset
func countLines(lines []string, offset int) int {
	currentOffset := 0
	for i, line := range lines {
		if currentOffset >= offset {
			return i
		}
		currentOffset += len(line) + 1 // +1 for newline
	}
	return len(lines)
}

// Helper function to get file extension for syntax highlighting
func getFileExtension(_ int) string {
	// This is a placeholder - in a real implementation you'd pass the filename
	return "txt"
}

// fitToWidth ensures text fits exactly within the specified width
func (dv *DiffViewer) fitToWidth(text string, width int) string {
	if width <= 0 {
		return ""
	}

	// Get the visual width (accounts for ANSI escape codes)
	visualWidth := ansi.PrintableRuneWidth(text)

	// Truncate if too long
	if visualWidth > width {
		return truncate.String(text, uint(width))
	}

	// Pad if too short
	if visualWidth < width {
		return text + strings.Repeat(" ", width-visualWidth)
	}

	return text
}

// wrapContentWithStyle wraps text content while preserving line prefix and background styling
func (dv *DiffViewer) wrapContentWithStyle(lineNum int, prefix string, content string, prefixStyle, contentBgStyle lipgloss.Style, contentWidth int) []string {
	if contentWidth <= 0 {
		return []string{""}
	}

	// Calculate the width available for actual content (excluding line number and prefix)
	lineNumStr := fmt.Sprintf("%4d", lineNum)
	prefixWithSeparator := prefix
	prefixVisualWidth := ansi.PrintableRuneWidth(lineNumStr + prefixWithSeparator)
	actualContentWidth := contentWidth - prefixVisualWidth

	if actualContentWidth <= 10 { // Minimum content width
		// If too narrow, just truncate
		styledLineNum := prefixStyle.Render(lineNumStr)
		styledPrefix := prefixStyle.Render(prefixWithSeparator)
		truncatedContent := truncate.String(content, uint(actualContentWidth))
		styledContent := contentBgStyle.Render(truncatedContent)
		return []string{styledLineNum + styledPrefix + styledContent}
	}

	// Wrap the content
	wrappedLines := wordwrap.String(content, actualContentWidth)
	lines := strings.Split(wrappedLines, "\n")

	var result []string
	for i, line := range lines {
		if i == 0 {
			// First line: include line number and prefix
			styledLineNum := prefixStyle.Render(lineNumStr)
			styledPrefix := prefixStyle.Render(prefixWithSeparator)
			styledContent := contentBgStyle.Render(line)
			result = append(result, styledLineNum+styledPrefix+styledContent)
		} else {
			// Continuation lines: padding + content
			padding := strings.Repeat(" ", prefixVisualWidth)
			styledContent := contentBgStyle.Render(line)
			result = append(result, padding+styledContent)
		}
	}

	return result
}
