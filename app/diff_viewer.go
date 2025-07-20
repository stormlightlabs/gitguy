package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/formatters"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/ansi"
	"github.com/muesli/reflow/truncate"
	"github.com/muesli/reflow/wordwrap"
)

// ViewMode represents the current viewing mode
type ViewMode int

const (
	UnifiedMode ViewMode = iota
	SideBySideMode
)

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
	LeftIndex   int // Index in leftLines (-1 if no corresponding line)
	RightIndex  int // Index in rightLines (-1 if no corresponding line)
	LogicalLine int // Logical line number for synchronization
}

// DiffViewer represents the diff viewer model
type DiffViewer struct {
	// Viewports for different modes
	leftViewport    viewport.Model
	rightViewport   viewport.Model
	unifiedViewport viewport.Model

	// Content and state
	content         string
	syntaxHighlight bool
	showWhitespace  bool
	width           int
	height          int
	files           []*gitdiff.File
	err             error

	// Synchronized state
	leftLines   []DiffLine
	rightLines  []DiffLine
	lineMapping []LineMapping

	// Current state
	mode       ViewMode
	scrollSync bool
}

// NewDiffViewer creates a new diff viewer with the given content and options
func NewDiffViewer(content string, sideBySide, syntaxHighlight, showWhitespace bool) *DiffViewer {
	// Parse the diff content
	files, _, err := gitdiff.Parse(strings.NewReader(content))

	// Initialize viewports
	leftVp := viewport.New(0, 0)
	rightVp := viewport.New(0, 0)
	unifiedVp := viewport.New(0, 0)

	// Determine initial mode
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
		files:           files,
		err:             err,
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

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Render("Git Diff Viewer")

	// Determine actual mode based on terminal width
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

		// Interleave the content naturally
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

			// Only show the separator if we have content on at least one side
			if left != "" || right != "" {
				line := left + " │ " + right
				b.WriteString(line + "\n")
			}
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

// calculatePaneWidths determines the width allocation for left and right panes
func (dv *DiffViewer) calculatePaneWidths() (leftWidth, rightWidth int) {
	available := dv.width - 3 // Reserve for separator " │ "
	leftWidth = available / 2
	rightWidth = available - leftWidth
	return leftWidth, rightWidth
}

// renderDiff renders the diff content based on current settings
func (dv *DiffViewer) renderDiff() {
	if dv.err != nil {
		errorContent := "Error parsing diff: " + dv.err.Error()
		dv.unifiedViewport.SetContent(errorContent)
		dv.leftViewport.SetContent(errorContent)
		dv.rightViewport.SetContent(errorContent)
		return
	}

	if len(dv.files) == 0 {
		noContent := "No diff content to display"
		dv.unifiedViewport.SetContent(noContent)
		dv.leftViewport.SetContent(noContent)
		dv.rightViewport.SetContent(noContent)
		return
	}

	if dv.mode == SideBySideMode {
		// TODO: Implement dual pane rendering
		dv.renderSideBySidePanes()
	} else {
		// Render unified view (existing logic)
		var content strings.Builder
		for _, file := range dv.files {
			content.WriteString(dv.renderFile(file))
			content.WriteString("\n")
		}
		dv.unifiedViewport.SetContent(content.String())
	}
}

// renderSideBySidePanes renders content for dual viewports
func (dv *DiffViewer) renderSideBySidePanes() {
	leftWidth, rightWidth := dv.calculatePaneWidths()

	var leftContent, rightContent strings.Builder

	for fileIndex, file := range dv.files {
		// Render file header for both sides
		fileHeader := dv.renderFileHeader(file)
		leftContent.WriteString(fileHeader)
		rightContent.WriteString(fileHeader)

		// Process each fragment
		for _, fragment := range file.TextFragments {
			filename := file.NewName
			if filename == "" {
				filename = file.OldName
			}
			leftLines, rightLines := dv.parseFragmentForSideBySide(fragment, leftWidth, rightWidth, filename)

			// Fragment header
			fragmentHeader := dv.renderFragmentHeader(fragment)
			leftContent.WriteString(fragmentHeader)
			rightContent.WriteString(fragmentHeader)

			// Render lines for each side
			for _, line := range leftLines {
				leftContent.WriteString(line + "\n")
			}
			for _, line := range rightLines {
				rightContent.WriteString(line + "\n")
			}
		}

		// Only add separator between files, not after the last file
		if fileIndex < len(dv.files)-1 {
			// Add a visual separator line between files
			separatorStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Faint(true)
			separator := separatorStyle.Render(strings.Repeat("─", leftWidth/2))
			leftContent.WriteString("\n" + separator + "\n")
			rightContent.WriteString("\n" + separator + "\n")
		}
	}

	dv.leftViewport.SetContent(leftContent.String())
	dv.rightViewport.SetContent(rightContent.String())
}

// parseFragmentForSideBySide parses a diff fragment into separate left/right line arrays
func (dv *DiffViewer) parseFragmentForSideBySide(fragment *gitdiff.TextFragment, _, _ int, filename string) ([]string, []string) {
	var leftLines, rightLines []string

	// Track line numbers for both sides
	leftLineNum := fragment.OldPosition
	rightLineNum := fragment.NewPosition

	for _, line := range fragment.Lines {
		// Skip whitespace-only changes if showWhitespace is false
		if !dv.showWhitespace && dv.isWhitespaceOnlyChange(line) {
			// Still need to increment line numbers for proper tracking
			switch line.Op {
			case gitdiff.OpDelete:
				leftLineNum++
			case gitdiff.OpAdd:
				rightLineNum++
			case gitdiff.OpContext:
				leftLineNum++
				rightLineNum++
			}
			continue
		}

		switch line.Op {
		case gitdiff.OpDelete:
			// Only show on left side with line number
			content := line.Line
			if dv.syntaxHighlight && filename != "" {
				content = dv.applySyntaxHighlighting(content, filename)
			}

			deleteStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).
				Background(lipgloss.Color("52"))

			// Format with line number
			lineNumStr := fmt.Sprintf("│%4d│", leftLineNum)
			displayLine := lineNumStr + deleteStyle.Render("-"+content)
			leftLines = append(leftLines, displayLine)
			leftLineNum++

		case gitdiff.OpAdd:
			// Only show on right side with line number
			content := line.Line
			if dv.syntaxHighlight && filename != "" {
				content = dv.applySyntaxHighlighting(content, filename)
			}

			addStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("46")).
				Background(lipgloss.Color("22"))

			// Format with line number
			lineNumStr := fmt.Sprintf("│%4d│", rightLineNum)
			displayLine := lineNumStr + addStyle.Render("+"+content)
			rightLines = append(rightLines, displayLine)
			rightLineNum++

		case gitdiff.OpContext:
			// Show on both sides with line numbers
			content := line.Line
			if dv.syntaxHighlight && filename != "" {
				content = dv.applySyntaxHighlighting(content, filename)
			}

			// Format with line numbers for both sides
			leftLineNumStr := fmt.Sprintf("│%4d│", leftLineNum)
			rightLineNumStr := fmt.Sprintf("│%4d│", rightLineNum)

			leftDisplayLine := leftLineNumStr + " " + content
			rightDisplayLine := rightLineNumStr + " " + content

			leftLines = append(leftLines, leftDisplayLine)
			rightLines = append(rightLines, rightDisplayLine)

			leftLineNum++
			rightLineNum++
		}
	}

	return leftLines, rightLines
}

// renderFileHeader renders just the file header portion
func (dv *DiffViewer) renderFileHeader(file *gitdiff.File) string {
	var b strings.Builder

	// File header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("33")).
		Background(lipgloss.Color("236"))

	oldName := file.OldName
	newName := file.NewName
	if oldName == "" {
		oldName = "/dev/null"
	}
	if newName == "" {
		newName = "/dev/null"
	}

	header := fmt.Sprintf(" diff --git a/%s b/%s ", oldName, newName)
	b.WriteString(headerStyle.Render(header) + "\n")

	// File mode changes
	if file.OldMode != 0 || file.NewMode != 0 {
		modeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
		b.WriteString(modeStyle.Render(fmt.Sprintf("old mode %o", file.OldMode)) + "\n")
		b.WriteString(modeStyle.Render(fmt.Sprintf("new mode %o", file.NewMode)) + "\n")
	}

	// File paths
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	if file.NewName != "" {
		b.WriteString(pathStyle.Render("--- a/"+oldName) + "\n")
		b.WriteString(pathStyle.Render("+++ b/"+newName) + "\n")
	}

	return b.String()
}

// renderFragmentHeader renders just the fragment header portion
func (dv *DiffViewer) renderFragmentHeader(fragment *gitdiff.TextFragment) string {
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("6")).
		Bold(true)

	header := fmt.Sprintf("@@ -%d,%d +%d,%d @@",
		fragment.OldPosition, fragment.OldLines,
		fragment.NewPosition, fragment.NewLines)

	result := headerStyle.Render(header)
	if fragment.Comment != "" {
		result += " " + fragment.Comment
	}
	return result + "\n"
}

// renderFile renders a single file diff
func (dv *DiffViewer) renderFile(file *gitdiff.File) string {
	var b strings.Builder

	// File header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("33")).
		Background(lipgloss.Color("236"))

	oldName := file.OldName
	newName := file.NewName
	if oldName == "" {
		oldName = "/dev/null"
	}
	if newName == "" {
		newName = "/dev/null"
	}

	header := fmt.Sprintf(" diff --git a/%s b/%s ", oldName, newName)
	b.WriteString(headerStyle.Render(header) + "\n")

	// File mode changes
	if file.OldMode != 0 || file.NewMode != 0 {
		modeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
		b.WriteString(modeStyle.Render(fmt.Sprintf("old mode %o", file.OldMode)) + "\n")
		b.WriteString(modeStyle.Render(fmt.Sprintf("new mode %o", file.NewMode)) + "\n")
	}

	// Index line - skip for now as go-gitdiff doesn't expose hash fields directly
	// We'll show the file path instead
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	if file.NewName != "" {
		b.WriteString(pathStyle.Render("--- a/"+oldName) + "\n")
		b.WriteString(pathStyle.Render("+++ b/"+newName) + "\n")
	}

	// Render fragments
	for _, fragment := range file.TextFragments {
		b.WriteString(dv.renderFragment(fragment, newName))
	}

	return b.String()
}

// renderFragment renders a diff fragment
func (dv *DiffViewer) renderFragment(fragment *gitdiff.TextFragment, filename string) string {
	var b strings.Builder

	// Fragment header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("6")).
		Bold(true)

	header := fmt.Sprintf("@@ -%d,%d +%d,%d @@",
		fragment.OldPosition, fragment.OldLines,
		fragment.NewPosition, fragment.NewLines)

	b.WriteString(headerStyle.Render(header))
	if fragment.Comment != "" {
		b.WriteString(" " + fragment.Comment)
	}
	b.WriteString("\n")

	if dv.mode == SideBySideMode {
		return b.String() + dv.renderSideBySide(fragment, filename)
	} else {
		return b.String() + dv.renderUnified(fragment, filename)
	}
}

// renderUnified renders the fragment in unified format
func (dv *DiffViewer) renderUnified(fragment *gitdiff.TextFragment, filename string) string {
	var b strings.Builder

	// Get available width for unified mode
	_, availableWidth, _ := dv.calculateLayout()

	for _, line := range fragment.Lines {
		// Skip whitespace-only changes if showWhitespace is false
		if !dv.showWhitespace && dv.isWhitespaceOnlyChange(line) {
			continue
		}

		styledLine := dv.styleDiffLine(line, filename)

		// Check if line needs wrapping using reflow's ANSI-aware length check
		if ansi.PrintableRuneWidth(styledLine) <= availableWidth {
			b.WriteString(styledLine + "\n")
		} else {
			// Wrap long lines using reflow
			content := line.Line
			wrappedLines := wrapText(content, availableWidth-1) // Reserve space for prefix

			for _, wrapped := range wrappedLines {
				// All lines get styled with the original operation
				b.WriteString(dv.styleDiffLine(gitdiff.Line{Op: line.Op, Line: wrapped}, filename) + "\n")
			}
		}
	}

	return b.String()
}

// renderSideBySide renders the fragment in side-by-side format
func (dv *DiffViewer) renderSideBySide(fragment *gitdiff.TextFragment, filename string) string {
	// Check if we should use side-by-side layout based on terminal width
	useSideBySide, leftWidth, rightWidth := dv.calculateLayout()
	if !useSideBySide {
		// Terminal too narrow, fall back to unified mode
		return dv.renderUnified(fragment, filename)
	}

	var leftLines, rightLines []string

	for _, line := range fragment.Lines {
		switch line.Op {
		case gitdiff.OpDelete:
			// Handle empty lines specially
			if strings.TrimSpace(line.Line) == "" {
				deleteStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("196")).
					Background(lipgloss.Color("52"))
				displayLine := deleteStyle.Render("-")
				leftLines = append(leftLines, displayLine)
				rightLines = append(rightLines, "")
			} else {
				// Work with the raw content, not the styled line
				content := line.Line
				wrappedLines := wrapText(content, leftWidth-1) // Reserve space for "-" prefix

				deleteStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("196")).
					Background(lipgloss.Color("52"))

				for _, wrapped := range wrappedLines {
					displayLine := deleteStyle.Render("-" + wrapped)
					leftLines = append(leftLines, displayLine)
					rightLines = append(rightLines, "")
				}
			}

		case gitdiff.OpAdd:
			// Handle empty lines specially
			if strings.TrimSpace(line.Line) == "" {
				addStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("46")).
					Background(lipgloss.Color("22"))
				displayLine := addStyle.Render("+")
				leftLines = append(leftLines, "")
				rightLines = append(rightLines, displayLine)
			} else {
				// Work with the raw content, not the styled line
				content := line.Line
				wrappedLines := wrapText(content, rightWidth-1) // Reserve space for "+" prefix

				addStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("46")).
					Background(lipgloss.Color("22"))

				for _, wrapped := range wrappedLines {
					leftLines = append(leftLines, "")
					displayLine := addStyle.Render("+" + wrapped)
					rightLines = append(rightLines, displayLine)
				}
			}

		case gitdiff.OpContext:
			// Handle empty lines specially
			if strings.TrimSpace(line.Line) == "" {
				displayLine := " "
				leftLines = append(leftLines, displayLine)
				rightLines = append(rightLines, displayLine)
			} else {
				// Work with the raw content, not the styled line
				content := line.Line
				maxWidth := minInt(leftWidth, rightWidth) - 1 // Reserve space for " " prefix
				wrappedLines := wrapText(content, maxWidth)

				for _, wrapped := range wrappedLines {
					displayLine := " " + wrapped
					leftLines = append(leftLines, displayLine)
					rightLines = append(rightLines, displayLine)
				}
			}
		}
	}

	// Balance the lines
	for len(leftLines) < len(rightLines) {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < len(leftLines) {
		rightLines = append(rightLines, "")
	}

	var b strings.Builder

	for i := 0; i < len(leftLines); i++ {
		left := leftLines[i]
		right := rightLines[i]

		// Ensure exact visual width using a more direct approach
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

		// Now both sides should have exact visual width
		line := left + " │ " + right
		b.WriteString(line + "\n")
	}

	return b.String()
}

// styleDiffLine applies styling to a diff line
func (dv *DiffViewer) styleDiffLine(line gitdiff.Line, filename string) string {
	content := line.Line

	// Apply syntax highlighting if enabled
	if dv.syntaxHighlight && line.Op == gitdiff.OpContext {
		content = dv.applySyntaxHighlighting(content, filename)
	}

	// Apply diff-specific styling
	switch line.Op {
	case gitdiff.OpDelete:
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Background(lipgloss.Color("52"))
		return style.Render("-" + content)
	case gitdiff.OpAdd:
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Background(lipgloss.Color("22"))
		return style.Render("+" + content)
	case gitdiff.OpContext:
		return " " + content
	default:
		return content
	}
}

// applySyntaxHighlighting applies syntax highlighting to content
func (dv *DiffViewer) applySyntaxHighlighting(content, filename string) string {
	if !dv.syntaxHighlight {
		return content
	}

	// Get lexer based on filename
	lexer := lexers.Match(filename)
	if lexer == nil {
		ext := filepath.Ext(filename)
		lexer = lexers.Get(ext[1:]) // Remove the dot
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

// Terminal width and layout utilities

// minInt returns the minimum of two integers
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// wrapText wraps text to fit within the specified width using reflow
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}

	wrapped := wordwrap.String(text, width)
	return strings.Split(wrapped, "\n")
}

// calculateLayout determines the optimal layout based on terminal width
func (dv *DiffViewer) calculateLayout() (bool, int, int) {
	const (
		minTerminalWidth   = 60
		minSideBySideWidth = 100
		dividerWidth       = 3 // " │ "
		margins            = 4 // Account for viewport margins
	)

	availableWidth := dv.width - margins

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

// isWhitespaceOnlyChange determines if a line contains only whitespace changes
func (dv *DiffViewer) isWhitespaceOnlyChange(line gitdiff.Line) bool {
	// Check if the line contains only whitespace (including empty lines)
	content := strings.TrimSpace(line.Line)
	return content == ""
}
