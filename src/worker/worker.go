package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-project/harness/log"
	"github.com/agent-project/harness/queue"
	"github.com/agent-project/harness/session"
	"github.com/agent-project/harness/webhook"
)

// Processor is the interface for message processing (implemented by agent.Agent).
type Processor interface {
	Process(ctx context.Context, sess *session.Session, messageText, systemPrompt string, imageAtt session.ImageAttachment) (string, error)
}

// promptFiles lists the workspace markdown files loaded into the system prompt,
// in order of priority (earlier = higher priority in the prompt).
var promptFiles = []string{
	"AGENTS.md",
	"SOUL.md",
	"IDENTITY.md",
	"USER.md",
	"MEMORY.md",
}

// Worker is a single worker that drains the FIFO queue and processes messages.
type Worker struct {
	q            *queue.Queue
	sessions     *session.Manager
	processor    Processor
	basePrompt   string
	workingDir   string
	pollInterval time.Duration
	logger       *log.Logger
}

// New creates a new Worker.
// basePrompt is the system prompt from the INI config (required).
// workingDir is the sandbox root where prompt files may reside.
// logger may be nil (logging calls are no-ops).
func New(q *queue.Queue, sessions *session.Manager, processor Processor, basePrompt, workingDir string, logger *log.Logger) *Worker {
	return &Worker{
		q:            q,
		sessions:     sessions,
		processor:    processor,
		basePrompt:   basePrompt,
		workingDir:   workingDir,
		pollInterval: 100 * time.Millisecond,
		logger:       logger,
	}
}

// buildSystemPrompt composes the full system prompt by starting with the base
// prompt from config and appending any workspace prompt files found in workingDir.
// Each file is wrapped with --- FILENAME --- delimiters.
func (w *Worker) buildSystemPrompt() string {
	var sb strings.Builder
	sb.WriteString(w.basePrompt)

	for _, fname := range promptFiles {
		path := filepath.Join(w.workingDir, fname)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // skip missing files
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue // skip empty files
		}
		sb.WriteString(fmt.Sprintf("\n\n--- %s ---\n%s\n--- END %s ---", fname, content, fname))
	}

	return sb.String()
}

// Run starts the worker loop. It blocks until ctx is cancelled.
// Messages are dequeued one at a time, processed through the agent, and
// the result is sent via the callback URL if present.
func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			if w.logger != nil {
				w.logger.Info("worker stopping")
			}
			return
		default:
		}

		msg, ok := w.q.Dequeue()
		if !ok {
			// Queue is empty, poll again after a short delay
			time.Sleep(w.pollInterval)
			continue
		}

		// Process the message
		w.processMessage(ctx, msg)
	}
}

// processMessage handles a single message: agent processing, session save, callback.
func (w *Worker) processMessage(ctx context.Context, msg queue.Message) {
	logger := w.logger

	if logger != nil {
		logger.Info("processing message",
			"channel", msg.ChannelID,
			"callback_url", msg.CallbackURL,
		)
	}

	// Get or create session for this channel
	sess := w.sessions.Get(msg.ChannelID)

	// Process through the agent with composed system prompt
	output, err := w.processor.Process(ctx, sess, msg.MessageText, w.buildSystemPrompt(), msg.ImageAttachment)
	if err != nil {
		if logger != nil {
			logger.Error("agent processing failed",
				"channel", msg.ChannelID,
				"error", err.Error(),
			)
		}
		// Send error as callback if URL is present
		if msg.CallbackURL != "" {
			callbackMsg := "Error: " + err.Error()
			if output != "" {
				callbackMsg += "\n\nPartial output:\n" + output
			}
			_ = webhook.SendCallback(msg.ChannelID, callbackMsg, msg.CallbackURL, w.logger)
		}
		// Save session to persist the user message before returning
		if err := w.sessions.Save(sess); err != nil {
			if logger != nil {
				logger.Error("failed to save session on error",
					"channel", msg.ChannelID,
					"error", err.Error(),
				)
			}
		}
		return
	}

	// Save session state
	if err := w.sessions.Save(sess); err != nil {
		if logger != nil {
			logger.Error("failed to save session",
				"channel", msg.ChannelID,
				"error", err.Error(),
			)
		}
	}

	// Send callback if URL is present
	if msg.CallbackURL != "" {
		if err := webhook.SendCallback(msg.ChannelID, output, msg.CallbackURL, w.logger); err != nil {
			if logger != nil {
				logger.Error("callback failed",
					"channel", msg.ChannelID,
					"error", err.Error(),
				)
			}
		}
	}
}
