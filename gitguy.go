package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/fang"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"stormlightlabs.org/gitguy/app"
)

const templateVar string = "PR_{{ID}}.md"

var (
	refCurrent     string
	refIncoming    string
	outPR          string
	nonInteractive bool
	apiKey         string
	prTemplate     string
	model          string
	
	// diff command flags
	sideBySide       bool
	unified          bool
	staged           bool
	unstaged         bool
	syntaxHighlight  bool
	showWhitespace   bool
)

// main is the entry point of the application.
// It sets up the root cobra command, flags, and configuration, then executes the command.
func main() {
	log.SetLevel(log.InfoLevel)

	var rootCmd = &cobra.Command{
		Use:   "gitguy",
		Short: "Generate commit messages and PR descriptions from Git diffs",
		Long:  "Interactive TUI tool for generating commit messages and PR descriptions using OpenRouter AI",
		RunE:  run,
	}

	var diffCmd = &cobra.Command{
		Use:   "diff",
		Short: "Show git diff with syntax highlighting",
		Long:  "Display git diff in side-by-side or unified format with syntax highlighting",
		RunE:  runDiff,
	}

	rootCmd.Flags().StringVar(&refCurrent, "ref-current", "", "Current Git ref (branch or commit SHA)")
	rootCmd.Flags().StringVar(&refIncoming, "ref-incoming", "", "Incoming Git ref (branch or commit SHA)")
	rootCmd.Flags().StringVar(&outPR, "out-pr", templateVar, "Output file for PR description")
	rootCmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Skip TUI and run in non-interactive mode")
	rootCmd.Flags().StringVar(&apiKey, "api-key", "", "OpenRouter API key")
	rootCmd.Flags().StringVar(&prTemplate, "pr-template", "", "Path to PR template markdown file")
	rootCmd.Flags().StringVar(&model, "model", app.DeepseekV3.String(), "LLM model to use (deepseek-v3, deepseek-r1, deepseek-r1-0528, kimi-k2)")

	// diff command flags
	diffCmd.Flags().BoolVar(&sideBySide, "side-by-side", true, "Display diff in side-by-side format")
	diffCmd.Flags().BoolVar(&unified, "unified", false, "Display diff in unified format")
	diffCmd.Flags().BoolVar(&staged, "staged", false, "Show staged changes")
	diffCmd.Flags().BoolVar(&unstaged, "unstaged", false, "Show unstaged changes")
	diffCmd.Flags().BoolVar(&syntaxHighlight, "syntax-highlighting", true, "Enable syntax highlighting")
	diffCmd.Flags().BoolVar(&showWhitespace, "whitespace", false, "Show whitespace changes (default: false)")

	viper.BindPFlag("ref-current", rootCmd.Flags().Lookup("ref-current"))
	viper.BindPFlag("ref-incoming", rootCmd.Flags().Lookup("ref-incoming"))
	viper.BindPFlag("out-pr", rootCmd.Flags().Lookup("out-pr"))
	viper.BindPFlag("non-interactive", rootCmd.Flags().Lookup("non-interactive"))
	viper.BindPFlag("api-key", rootCmd.Flags().Lookup("api-key"))
	viper.BindPFlag("pr-template", rootCmd.Flags().Lookup("pr-template"))
	viper.BindPFlag("model", rootCmd.Flags().Lookup("model"))

	rootCmd.AddCommand(diffCmd)

	viper.AutomaticEnv()

	if err := app.SetupConfig(); err != nil {
		log.Error("Error setting up config", "error", err)
	}

	if err := fang.Execute(context.Background(), rootCmd); err != nil {
		log.Error("Command failed", "error", err)
		os.Exit(1)
	}
}

// run determines whether to run the application in interactive or non-interactive mode
// based on the `--non-interactive` flag.
func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if _, err := app.OpenRepo("."); err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	if viper.GetBool("non-interactive") {
		return runNonInteractive(ctx)
	}

	return runInteractive(ctx)
}

// runNonInteractive executes the non-interactive mode of the application.
// It generates a diff, calls the LLM to get a commit message and PR description,
// prints the commit message to stdout, and saves the PR description to a file.
func runNonInteractive(_ context.Context) error {
	log.Info("Running in non-interactive mode")

	refCurrent := viper.GetString("ref-current")
	refIncoming := viper.GetString("ref-incoming")
	outPR := viper.GetString("out-pr")

	if refCurrent == "" || refIncoming == "" {
		return fmt.Errorf("both --ref-current and --ref-incoming are required in non-interactive mode")
	}

	repo, _ := app.OpenRepo(".")

	diff, err := repo.GetDiff(refCurrent, refIncoming)
	if err != nil {
		return fmt.Errorf("failed to generate diff: %w", err)
	}

	if strings.TrimSpace(diff) == "" {
		return fmt.Errorf("no differences found between %s and %s", refCurrent, refIncoming)
	}

	result, err := app.GenerateCommitAndPR(diff)
	if err != nil {
		return fmt.Errorf("failed to generate commit and PR: %w", err)
	}

	// Print commit message to stdout
	fmt.Println(result.CommitMessage)

	// Expand template if it is pr_{{id}}.md or PR_{{ID}}.md
	if strings.ToUpper(outPR) == templateVar {
		outPR = app.ExpandPRTemplate(outPR)
	}

	frontMatter := fmt.Sprintf(`---
title: "%s"
base: %s
head: %s
---

`, result.CommitMessage, refCurrent, refIncoming)

	content := frontMatter + result.PRDescription

	// Atomic write
	tempFile := outPR + ".tmp"
	err = os.WriteFile(tempFile, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	err = os.Rename(tempFile, outPR)
	if err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to write PR file: %w", err)
	}

	log.Info("PR description written", "file", outPR)
	return nil
}

// runInteractive starts the interactive TUI for the application.
func runInteractive(_ context.Context) error {
	log.Info("Starting interactive TUI")

	p := tea.NewProgram(app.Init(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// runDiff executes the diff command with the specified options.
func runDiff(cmd *cobra.Command, args []string) error {
	repo, err := app.OpenRepo(".")
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	// Determine what diff to show based on flags
	var diffContent string
	if staged {
		diffContent, err = repo.GetStagedDiff()
		if err != nil {
			return fmt.Errorf("failed to get staged diff: %w", err)
		}
		if strings.TrimSpace(diffContent) == "" {
			return fmt.Errorf("no staged changes found")
		}
	} else if unstaged {
		diffContent, err = repo.GetUnstagedDiff()
		if err != nil {
			return fmt.Errorf("failed to get unstaged diff: %w", err)
		}
		if strings.TrimSpace(diffContent) == "" {
			return fmt.Errorf("no unstaged changes found")
		}
	} else {
		// Default: try unstaged first, then staged
		diffContent, err = repo.GetUnstagedDiff()
		if err == nil && strings.TrimSpace(diffContent) != "" {
			// Found unstaged changes
		} else {
			// Try staged changes
			diffContent, err = repo.GetStagedDiff()
			if err != nil {
				return fmt.Errorf("failed to get diff: %w", err)
			}
			if strings.TrimSpace(diffContent) == "" {
				return fmt.Errorf("no changes found (neither staged nor unstaged)")
			}
		}
	}

	// Create and run the diff viewer
	diffViewer := app.NewDiffViewer(diffContent, sideBySide, syntaxHighlight, showWhitespace)
	p := tea.NewProgram(diffViewer, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
