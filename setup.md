# Implementation Plan: Enhanced API Key Management with Setup Command

Based on my analysis of the current configuration system and security best practices, here's my plan to enhance API key storage with a dedicated setup command:

## Current State Analysis

The codebase already has excellent XDG_CONFIG/APPDATA directory handling in `app/config.go`:

- ✅ Cross-platform directory detection (Windows: APPDATA, Unix: XDG_CONFIG_HOME)
- ✅ Fallback to standard directories (~/.config/gitguy, %APPDATA%\gitguy)
- ✅ YAML configuration file support via Viper
- ✅ `SaveAPIKey()` function exists but needs to be exposed via CLI

## Proposed Enhancements

### 1. Add Setup Command

- Create `gitguy setup` subcommand for initial configuration
- Interactive prompts for API key entry with validation
- Optional flags for non-interactive setup: `gitguy setup --api-key <key>`
- Verification of API key by making test request to OpenRouter
- Success confirmation with configuration location

### 2. Enhanced Configuration Management

- Add `gitguy config` subcommand for managing all settings:
    - `gitguy config get api-key` (show status, not actual key)
    - `gitguy config set api-key <value>`
    - `gitguy config show` (display current config file location and settings)
    - `gitguy config reset` (clear stored API key)

### 3. Security Improvements

- Set restrictive file permissions (0600) on config files containing API keys
- Add option to encrypt API keys in config file using system keyring integration
- Implement key rotation reminder/warning for old keys
- Add validation for API key format before saving

### 4. User Experience Enhancements

- Better error messages when API key is missing/invalid
- Guided setup flow for first-time users
- Support for multiple API providers (future-proofing)
- Configuration backup/restore functionality

## Implementation Steps

1. Add `setup` and `config` subcommands to cobra CLI in `gitguy.go`
2. Enhance `app/config.go` with new functions for setup workflow
3. Add API key validation via test OpenRouter request
4. Implement secure file permissions and optional encryption
5. Create interactive prompts using existing Bubble Tea components
6. Update documentation and help text

## File Changes

- `gitguy.go`: Add new cobra commands
- `app/config.go`: Enhanced configuration functions with security
- `app/setup.go`: New file for setup workflow logic
- `README.md`: Updated setup instructions

## Example Usage

### Initial Setup

```bash
# Interactive setup
gitguy setup

# Non-interactive setup
gitguy setup --api-key sk-or-v1-abcd1234...
```

### Configuration Management

```bash
# Check current configuration
gitguy config show

# Set API key
gitguy config set api-key sk-or-v1-abcd1234...

# Check API key status (without revealing the key)
gitguy config get api-key

# Reset configuration
gitguy config reset
```

## Security Considerations

### File Permissions

Configuration files containing API keys will be created with restrictive permissions (0600) to prevent unauthorized access.

### API Key Validation

Before saving, API keys will be validated by making a test request to the OpenRouter API to ensure they are valid and functional.

### Storage Location

Following XDG Base Directory Specification:

- Linux/macOS: `~/.config/gitguy/config.yaml`
- Windows: `%APPDATA%\gitguy\config.yaml`

### Environment Variable Hierarchy

1. Command-line flag (`--api-key`)
2. Environment variable (`OPENROUTER_API_KEY`)
3. Configuration file

This approach leverages the existing robust configuration system while adding user-friendly setup workflows and enhanced security measures.
