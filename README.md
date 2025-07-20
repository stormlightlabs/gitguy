# `gitguy`

`gitguy` is a command-line tool that uses AI to generate commit messages and pull request
descriptions from your git diffs. It can be run as an interactive TUI or as a non-interactive
CLI for use in scripts and CI/CD pipelines.

## Features

- **Interactive TUI**: A user-friendly terminal interface for selecting git references (branches or commits) and viewing generated content.
- **Non-Interactive Mode**: Generate commit messages and PRs directly from the command line, perfect for automation.
- **AI-Powered**: Leverages OpenRouter to generate high-quality, conventional commit messages and detailed PR descriptions.
- **Customizable**: Use your own PR templates to guide the AI's output.
- **Secure**: API keys are stored securely in your system's default configuration directory.
- **Logging**: Detailed logs of API requests are stored for debugging and auditing.

## Installation

```bash
go get github.com/stormlightlabs/gitguy
```

## Usage

### Interactive Mode

To start the interactive TUI, simply run `gitguy` in your git repository:

```bash
gitguy
```

This will launch a TUI where you can:

1. **Select "current" and "incoming" refs** (branches or commits) to generate a diff.
2. **View the generated diff**.
3. **Generate a commit message and PR description** from the diff.
4. **Copy** the commit message or **save** the PR description to a file.

### Non-Interactive Mode

For scripting or CI integration, you can use the following flags:

```bash
gitguy \
  --ref-current <current-ref> \
  --ref-incoming <incoming-ref> \
  --out-pr PR.md \
  --non-interactive
```

- `--ref-current`: The base git reference (e.g., `main`, `HEAD`).
- `--ref-incoming`: The feature branch or commit to compare.
- `--out-pr`: The output file for the PR description (defaults to `PR.md`).
- `--non-interactive`: Skips the TUI and prints the commit message to stdout.

### Configuration

`gitguy` requires an OpenRouter API key. You can provide it in one of the following ways:

1. **Command-line flag**: `--api-key YOUR_API_KEY`
2. **Environment variable**: `OPENROUTER_API_KEY=YOUR_API_KEY`
3. **Configuration file**: The first time you run `gitguy`, it will create a `config.yaml` file in the appropriate configuration directory for your OS (`~/.config/gitguy` on Linux, `~/Library/Application Support/gitguy` on macOS, and `%APPDATA%\gitguy` on Windows). You can add your API key to this file.

You can also specify a PR template file using the `--pr-template` flag.

## Architecture

`gitguy` is built with the following Go libraries:

- **CLI**: [Cobra](https://github.com/spf13/cobra) for command-line argument parsing.
- **TUI**: [Bubble Tea](https://github.com/charmbracelet/bubbletea) for the interactive terminal interface.
- **Git**: [go-git](https://github.com/go-git/go-git) for git repository operations.
- **Configuration**: [Viper](https://github.com/spf13/viper) for managing configuration.
- **Logging**: A custom logger that writes API interactions to a log file.

## TODO

1.
