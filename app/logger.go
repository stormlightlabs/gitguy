package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
)

// APIClientLogEntry represents a single log entry for an OpenRouter API call.
// It includes the request, response, and other metadata.
type APIClientLogEntry struct {
	Timestamp  time.Time     `json:"timestamp"`
	UUID       string        `json:"uuid"`
	Request    APIRequest    `json:"request"`
	Response   *APIResponse  `json:"response,omitempty"`
	Error      string        `json:"error,omitempty"`
	StatusCode int           `json:"status_code,omitempty"`
	Duration   time.Duration `json:"duration_ms"`
}

// APILogger provides a simple file-based logger for OpenRouter API calls.
// Log files are created in the application's configuration directory.
type APILogger struct {
	logFile *os.File
	logPath string
}

// NewAPILogger creates a new OpenRouterLogger and a corresponding log file.
// The log file is named with a UUID and timestamp to ensure uniqueness.
func NewAPILogger() (*APILogger, error) {
	// Get config directory or fallback to current working directory
	configDir := viper.GetString("config-dir")
	if configDir == "" {
		var err error
		configDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	logUUID := uuid.New().String()
	unixEpoch := time.Now().Unix()
	filename := fmt.Sprintf("gitguy_%s_%d.log", logUUID, unixEpoch)
	logPath := filepath.Join(configDir, filename)

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	return &APILogger{
		logFile: logFile,
		logPath: logPath,
	}, nil
}

// LogAPICall logs a single OpenRouter API call to the log file.
// It takes the request, response, error, status code, and duration of the call as input.
func (l *APILogger) LogAPICall(
	requestUUID string,
	request APIRequest,
	response *APIResponse,
	err error,
	statusCode int,
	duration time.Duration,
) {
	entry := APIClientLogEntry{
		Timestamp:  time.Now(),
		UUID:       requestUUID,
		Request:    request,
		Response:   response,
		StatusCode: statusCode,
		Duration:   duration,
	}

	if err != nil {
		entry.Error = err.Error()
	}

	jsonData, jsonErr := json.Marshal(entry)
	if jsonErr != nil {
		// If we can't marshal the entry, log a basic error entry
		fallbackEntry := map[string]interface{}{
			"timestamp": time.Now(),
			"uuid":      requestUUID,
			"error":     "failed to marshal log entry: " + jsonErr.Error(),
		}
		if fallbackData, fallbackErr := json.Marshal(fallbackEntry); fallbackErr == nil {
			l.logFile.Write(append(fallbackData, '\n'))
		}
		return
	}

	// Write JSON entry followed by newline
	l.logFile.Write(append(jsonData, '\n'))
	l.logFile.Sync() // Ensure data is written to disk
}

// Close closes the log file.
func (l *APILogger) Close() error {
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}

// GetLogPath returns the path to the current log file.
func (l *APILogger) GetLogPath() string {
	return l.logPath
}
