package app

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// getConfigDir determines the appropriate configuration directory based on the user's operating system.
// It follows standard conventions for each OS (e.g., XDG directories on Linux, AppData on Windows).
// If the directory doesn't exist, it will be created.
func getConfigDir() (string, error) {
	var configDir string

	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			configDir = filepath.Join(appData, "gitguy")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to get home directory: %w", err)
			}
			configDir = filepath.Join(home, ".gitguy")
		}
	case "darwin", "linux":
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig != "" {
			configDir = filepath.Join(xdgConfig, "gitguy")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to get home directory: %w", err)
			}
			configDir = filepath.Join(home, ".config", "gitguy")
		}
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		configDir = filepath.Join(home, ".gitguy")
	}

	// Ensure directory exists
	err := os.MkdirAll(configDir, 0755)
	if err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	return configDir, nil
}

// SetupConfig initializes Viper for configuration management.
// It sets up the configuration file name, type, and search paths.
// It also handles the case where a configuration file doesn't exist yet.
func SetupConfig() error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}

	// Set up viper to look in the platform-specific config directory
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(configDir)
	viper.AddConfigPath(".")

	// Set default config directory for future use
	viper.Set("config-dir", configDir)

	// Try to read existing config
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("error reading config file: %w", err)
		}
		// Config file doesn't exist, which is fine for first run
	}

	return nil
}

// SaveAPIKey saves the provided OpenRouter API key to the configuration file.
// TODO: add command to save
func SaveAPIKey(apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	configDir, err := getConfigDir()
	if err != nil {
		return err
	}

	viper.Set("api-key", apiKey)

	configFile := filepath.Join(configDir, "config.yaml")
	err = viper.WriteConfigAs(configFile)
	if err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// getAPIKey retrieves the OpenRouter API key from one of the following sources, in order of precedence:
// 1. Command-line flag (`--api-key`)
// 2. Environment variable (`OPENROUTER_API_KEY`)
// 3. Configuration file
func getAPIKey() string {
	// Check command line flag first
	if apiKey := viper.GetString("api-key"); apiKey != "" {
		return apiKey
	}

	// Check environment variable
	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		return apiKey
	}

	// Check config file
	return viper.GetString("api-key")
}

// ExpandPRTemplate expands the PR template by replacing {{ID}} with a random 8-bit integer
func ExpandPRTemplate(template string) string {
	if strings.Contains(template, "{{ID}}") {
		// Initialize random seed
		rand.Seed(time.Now().UnixNano())
		// Generate random 8-bit integer (0-255)
		randomID := rand.Intn(256)
		return strings.ReplaceAll(template, "{{ID}}", fmt.Sprintf("%d", randomID))
	}
	return template
}
