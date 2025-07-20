package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/viper"
)

// sessionState represents the current view of the TUI.
type sessionState int

const (
	refSelectionView sessionState = iota
	diffView
	resultView
)

// refSide represents which side of the diff is currently active in the ref selection view.
type refSide int

const (
	currentSide refSide = iota
	incomingSide
)

// model represents the state of the TUI application.
type model struct {
	state                sessionState
	repo                 *GitRepo
	currentRefList       list.Model
	incomingRefList      list.Model
	diffViewport         viewport.Model
	resultViewport       viewport.Model
	activeSide           refSide
	selectedCurrent      string
	selectedIncoming     string
	selectedCurrentName  string
	selectedIncomingName string
	diff                 string
	commitMessage        string
	prDescription        string
	width                int
	height               int
	err                  error
	lastKeypress         string
	keypressTimer        int
}

// refItem represents an item in the reference selection list.
type refItem struct {
	ref RefInfo
}

func (i refItem) FilterValue() string { return i.ref.Name }
func (i refItem) Title() string       { return i.ref.Name }
func (i refItem) Description() string { return fmt.Sprintf("%s (%s)", i.ref.Hash, i.ref.Type) }

// Init creates the initial model for the TUI application.
func Init() model {
	repo, err := OpenRepo(".")
	if err != nil {
		return model{err: err}
	}

	currentList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	currentList.Title = "Current Ref"

	incomingList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	incomingList.Title = "Incoming Ref"

	diffViewport := viewport.New(0, 0)
	resultViewport := viewport.New(0, 0)

	return model{
		state:           refSelectionView,
		repo:            repo,
		currentRefList:  currentList,
		incomingRefList: incomingList,
		diffViewport:    diffViewport,
		resultViewport:  resultViewport,
		activeSide:      currentSide,
	}
}

// Init initializes the TUI application.
func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.loadRefs(),
		tea.EnterAltScreen,
		tickCmd(),
	)
}

// tickCmd returns a command that will send a tickMsg after a short delay
func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// loadRefs loads the git references (branches, commits, and staged files) into the model.
func (m model) loadRefs() tea.Cmd {
	return func() tea.Msg {
		var items []list.Item

		// Add staged files if they exist
		stagedRef, err := m.repo.GetStagedRef()
		if err == nil && stagedRef != nil {
			items = append(items, refItem{ref: *stagedRef})
		}

		branches, err := m.repo.GetBranches()
		if err != nil {
			return errMsg{err}
		}

		commits, err := m.repo.GetRecentCommits(10)
		if err != nil {
			return errMsg{err}
		}

		for _, branch := range branches {
			items = append(items, refItem{ref: branch})
		}
		for _, commit := range commits {
			items = append(items, refItem{ref: commit})
		}

		return refsLoadedMsg{items}
	}
}

// refsLoadedMsg is a message that is sent when the git references have been loaded.
type refsLoadedMsg struct {
	items []list.Item
}

// errMsg is a message that is sent when an error occurs.
type errMsg struct {
	err error
}

func (e errMsg) Error() string {
	return e.err.Error()
}

// diffGeneratedMsg is a message that is sent when the git diff has been generated.
type diffGeneratedMsg struct {
	diff string
}

// llmResultMsg is a message that is sent when the LLM has generated a commit message and PR description.
type llmResultMsg struct {
	commitMessage string
	prDescription string
}

// tickMsg is sent periodically to update the keypress timer
type tickMsg time.Time

// Update handles messages and updates the model accordingly.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Adjust sizing to prevent cut-off - reserve more space for UI elements
		listWidth := m.width/2 - 3
		listHeight := m.height - 10 // Reserve more space for title, selections, status, and help

		m.currentRefList.SetSize(listWidth, listHeight)
		m.incomingRefList.SetSize(listWidth, listHeight)
		m.diffViewport.Width = m.width - 4
		m.diffViewport.Height = m.height - 8 // Reserve more space for navigation
		m.resultViewport.Width = m.width - 4
		m.resultViewport.Height = m.height - 8

	case tickMsg:
		// Update keypress timer
		if m.keypressTimer > 0 {
			m.keypressTimer--
		} else {
			m.lastKeypress = ""
		}
		return m, tickCmd()

	case refsLoadedMsg:
		m.currentRefList.SetItems(msg.items)
		m.incomingRefList.SetItems(msg.items)

	case errMsg:
		m.err = msg.err

	case diffGeneratedMsg:
		m.diff = msg.diff
		m.diffViewport.SetContent(msg.diff)
		m.state = diffView

	case llmResultMsg:
		m.commitMessage = msg.commitMessage
		m.prDescription = msg.prDescription
		content := fmt.Sprintf("COMMIT MESSAGE:\n%s\n\nPR DESCRIPTION:\n%s", msg.commitMessage, msg.prDescription)
		m.resultViewport.SetContent(content)
		m.state = resultView

	case tea.KeyMsg:
		// Record keypress for visual feedback
		keyStr := msg.String()
		switch keyStr {
		case " ":
			keyStr = "space"
		case "ctrl+c":
			keyStr = "ctrl+c"
		}
		m.lastKeypress = keyStr
		m.keypressTimer = 10 // Show for 1 second (10 * 100ms)

		switch m.state {
		case refSelectionView:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "tab":
				if m.activeSide == currentSide {
					m.activeSide = incomingSide
				} else {
					m.activeSide = currentSide
				}
			case "enter", " ":
				if m.activeSide == currentSide {
					if item, ok := m.currentRefList.SelectedItem().(refItem); ok {
						m.selectedCurrent = item.ref.Hash
						m.selectedCurrentName = item.ref.Name
					}
				} else {
					if item, ok := m.incomingRefList.SelectedItem().(refItem); ok {
						m.selectedIncoming = item.ref.Hash
						m.selectedIncomingName = item.ref.Name
					}
				}

				if m.selectedCurrent != "" && m.selectedIncoming != "" {
					return m, m.generateDiff()
				}
			case "r":
				// Reset selections
				if m.activeSide == currentSide {
					m.selectedCurrent = ""
					m.selectedCurrentName = ""
				} else {
					m.selectedIncoming = ""
					m.selectedIncomingName = ""
				}
			case "R":
				// Reset all selections
				m.selectedCurrent = ""
				m.selectedIncoming = ""
				m.selectedCurrentName = ""
				m.selectedIncomingName = ""
			}

		case diffView:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "b":
				m.state = refSelectionView
			case "g":
				return m, m.generateLLMResult()
			}

		case resultView:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "d":
				m.state = diffView
			case "c":
				return m, m.copyCommitMessage()
			case "p":
				return m, m.savePRDescription()
			}
		}
	}

	switch m.state {
	case refSelectionView:
		if m.activeSide == currentSide {
			m.currentRefList, cmd = m.currentRefList.Update(msg)
		} else {
			m.incomingRefList, cmd = m.incomingRefList.Update(msg)
		}
		cmds = append(cmds, cmd)

	case diffView:
		m.diffViewport, cmd = m.diffViewport.Update(msg)
		cmds = append(cmds, cmd)

	case resultView:
		m.resultViewport, cmd = m.resultViewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// generateDiff generates a git diff between the selected references.
func (m model) generateDiff() tea.Cmd {
	return func() tea.Msg {
		var diff string
		var err error

		// Handle special cases for staged files
		if m.selectedCurrent == "staged" {
			diff, err = m.repo.GetStagedDiff()
		} else if m.selectedIncoming == "staged" {
			diff, err = m.repo.GetStagedDiff()
		} else {
			diff, err = m.repo.GetDiff(m.selectedCurrent, m.selectedIncoming)
		}

		if err != nil {
			return errMsg{err}
		}
		return diffGeneratedMsg{diff}
	}
}

// generateLLMResult generates a commit message and PR description from the git diff.
func (m model) generateLLMResult() tea.Cmd {
	return func() tea.Msg {
		result, err := GenerateCommitAndPR(m.diff)
		if err != nil {
			return errMsg{err}
		}
		return llmResultMsg{
			commitMessage: result.CommitMessage,
			prDescription: result.PRDescription,
		}
	}
}

// copyCommitMessage copies the generated commit message to the clipboard.
func (m model) copyCommitMessage() tea.Cmd {
	return func() tea.Msg {
		err := clipboard.WriteAll(m.commitMessage)
		if err != nil {
			return errMsg{fmt.Errorf("failed to copy to clipboard: %w", err)}
		}
		return nil
	}
}

// savePRDescription saves the generated PR description to a file.
func (m model) savePRDescription() tea.Cmd {
	return func() tea.Msg {
		filename := viper.GetString("out-pr")
		if filename == "" {
			filename = "PR.md"
		}

		// Expand template if it contains {{ID}}
		filename = ExpandPRTemplate(filename)

		// Create front matter
		frontMatter := fmt.Sprintf(`---
title: "%s"
base: %s
head: %s
---

`, m.commitMessage, m.selectedCurrent[:8], m.selectedIncoming[:8])

		content := frontMatter + m.prDescription

		tempFile := filename + ".tmp"
		err := os.WriteFile(tempFile, []byte(content), 0644)
		if err != nil {
			return errMsg{fmt.Errorf("failed to write temp file: %w", err)}
		}

		err = os.Rename(tempFile, filename)
		if err != nil {
			os.Remove(tempFile)
			return errMsg{fmt.Errorf("failed to rename temp file: %w", err)}
		}

		return nil
	}
}

// View renders the TUI.
func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err)
	}

	switch m.state {
	case refSelectionView:
		return m.refSelectionView()
	case diffView:
		return m.diffView()
	case resultView:
		return m.resultView()
	}

	return ""
}

// refSelectionView renders the reference selection view.
func (m model) refSelectionView() string {
	var b strings.Builder

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Render("Select Git References")

	b.WriteString(title + "\n\n")

	// Create styles for the lists
	currentStyle := lipgloss.NewStyle().Border(lipgloss.NormalBorder())
	incomingStyle := lipgloss.NewStyle().Border(lipgloss.NormalBorder())

	if m.activeSide == currentSide {
		currentStyle = currentStyle.BorderForeground(lipgloss.Color("205"))
	} else {
		incomingStyle = incomingStyle.BorderForeground(lipgloss.Color("205"))
	}

	// Add title with selected indicator for current ref
	currentTitle := "Current Ref"
	if m.selectedCurrentName != "" {
		selectedStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Bold(true)
		currentTitle += "\n" + selectedStyle.Render("✓ Selected: "+m.selectedCurrentName)
	}
	m.currentRefList.Title = currentTitle

	// Add title with selected indicator for incoming ref
	incomingTitle := "Incoming Ref"
	if m.selectedIncomingName != "" {
		selectedStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Bold(true)
		incomingTitle += "\n" + selectedStyle.Render("✓ Selected: "+m.selectedIncomingName)
	}
	m.incomingRefList.Title = incomingTitle

	currentView := currentStyle.Render(m.currentRefList.View())
	incomingView := incomingStyle.Render(m.incomingRefList.View())

	content := lipgloss.JoinHorizontal(lipgloss.Top, currentView, "  ", incomingView)
	b.WriteString(content)

	// Show status and help
	var statusMsg string
	if m.selectedCurrentName != "" && m.selectedIncomingName != "" {
		statusMsg = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Bold(true).
			Render("✓ Ready to generate diff! Press Enter to continue.")
	} else {
		missing := []string{}
		if m.selectedCurrentName == "" {
			missing = append(missing, "current ref")
		}
		if m.selectedIncomingName == "" {
			missing = append(missing, "incoming ref")
		}
		statusMsg = fmt.Sprintf("Select %s to continue", strings.Join(missing, " and "))
	}

	b.WriteString("\n\n" + statusMsg)

	// Add keypress feedback
	helpLine := "Tab: Switch sides | Enter/Space: Select | r: Reset current | R: Reset all | q: Quit"
	if m.lastKeypress != "" && m.keypressTimer > 0 {
		keypressStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Bold(true).
			Background(lipgloss.Color("235"))
		keyFeedback := keypressStyle.Render(fmt.Sprintf(" Key: %s ", m.lastKeypress))
		helpLine = helpLine + "\n" + keyFeedback
	}

	b.WriteString("\n\n" + helpLine)

	return b.String()
}

// diffView renders the diff view.
func (m model) diffView() string {
	var b strings.Builder

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Render("Git Diff")

	b.WriteString(title + "\n\n")
	b.WriteString(m.diffViewport.View())

	helpLine := "j/k: Scroll | g: Generate commit & PR | b: Back | q: Quit"
	if m.lastKeypress != "" && m.keypressTimer > 0 {
		keypressStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Bold(true).
			Background(lipgloss.Color("235"))
		keyFeedback := keypressStyle.Render(fmt.Sprintf(" Key: %s ", m.lastKeypress))
		helpLine = helpLine + "\n" + keyFeedback
	}

	b.WriteString("\n\n" + helpLine)

	return b.String()
}

// resultView renders the result view.
func (m model) resultView() string {
	var b strings.Builder

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Render("Generated Commit & PR")

	b.WriteString(title + "\n\n")
	b.WriteString(m.resultViewport.View())

	// Add keypress feedback
	helpLine := "c: Copy commit | p: Save PR | d: Back to diff | q: Quit"
	if m.lastKeypress != "" && m.keypressTimer > 0 {
		keypressStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Bold(true).
			Background(lipgloss.Color("235"))
		keyFeedback := keypressStyle.Render(fmt.Sprintf(" Key: %s ", m.lastKeypress))
		helpLine = helpLine + "\n" + keyFeedback
	}

	b.WriteString("\n\n" + helpLine)

	return b.String()
}
