package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/agent-project/harness/log"
)

// CallbackPayload is the JSON body sent to the outbound callback URL.
type CallbackPayload struct {
	Channel string `json:"channel"`
	Message string `json:"message"`
}

// SendCallback POSTs the agent's response to the provided callback URL.
// On success (2xx), it returns nil.
// On failure, it logs at error level (channel ID + HTTP status) and returns the error.
// Full message details are only logged at debug level.
// logger may be nil (logging calls are no-ops).
func SendCallback(channelID, message, callbackURL string, logger *log.Logger) error {
	payload, err := json.Marshal(CallbackPayload{
		Channel: channelID,
		Message: message,
	})
	if err != nil {
		return fmt.Errorf("marshal callback: %w", err)
	}

	req, err := http.NewRequest("POST", callbackURL, bytes.NewReader(payload))
	if err != nil {
		if logger != nil {
			logger.WithSource("plugin.webhook").Error("callback request failed",
				"channel", channelID,
				"error", err.Error(),
			)
		}
		return fmt.Errorf("create callback request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Use a dedicated client with a timeout instead of http.DefaultClient.
	// This prevents hanging indefinitely if the callback URL is unreachable
	// and bounds the window for EOF errors when the client exits quickly.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if logger != nil {
			logger.WithSource("plugin.webhook").Error("callback network error",
				"channel", channelID,
				"error", err.Error(),
			)
			if logger.Level <= log.DebugLevel {
				logger.WithSource("plugin.webhook").Debug("callback payload", "channel", channelID, "message", message)
			}
		}
		return fmt.Errorf("callback network error: %w", err)
	}
	defer resp.Body.Close()

	// Read body for error logging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if logger != nil {
			logger.WithSource("plugin.webhook").Error("callback failed",
				"channel", channelID,
				"status", fmt.Sprintf("%d", resp.StatusCode),
			)
			if logger.Level <= log.DebugLevel {
				logger.WithSource("plugin.webhook").Debug("callback response", "channel", channelID, "body", string(body))
				logger.WithSource("plugin.webhook").Debug("callback payload", "channel", channelID, "message", message)
			}
		}
		return fmt.Errorf("callback returned status %d: %s", resp.StatusCode, string(body))
	}

	if logger != nil {
		logger.WithSource("plugin.webhook").Info("response sent",
			"channel", channelID,
			"tokens", fmt.Sprintf("%d", len(message)/4),
		)
	}

	return nil
}
