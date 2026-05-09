package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/agent-project/harness/channellog"
	"github.com/agent-project/harness/llm"
	"github.com/agent-project/harness/log"
	"github.com/agent-project/harness/session"
	"github.com/agent-project/harness/tools"
)

// ChatClient is the interface for LLM chat completions.
type ChatClient interface {
	Chat(ctx context.Context, messages []llm.Message, toolsJSON json.RawMessage, maxTokens int) (*llm.ChatResponse, error)
}

// Agent processes messages through the LLM with tool-call support.
type Agent struct {
	client              ChatClient
	tools               *tools.Registry
	maxToolIterations   int
	contextTokens       int
	summarizeThreshold  float64
	summarizeKeepRecent int
	maxTokens           int
	summaryPrompt       string
	logToolCalls        bool
	logAgentReasoning   bool
	channelLogger       *channellog.Logger
}

// New creates a new Agent.
func New(client ChatClient, reg *tools.Registry, maxToolIterations, contextTokens int, summarizeThreshold float64, summarizeKeepRecent, maxTokens int, summaryPrompt string, logToolCalls, logAgentReasoning bool, channelLogger *channellog.Logger) *Agent {
	return &Agent{
		client:              client,
		tools:               reg,
		maxToolIterations:   maxToolIterations,
		contextTokens:       contextTokens,
		summarizeThreshold:  summarizeThreshold,
		summarizeKeepRecent: summarizeKeepRecent,
		maxTokens:           maxTokens,
		summaryPrompt:       summaryPrompt,
		logToolCalls:        logToolCalls,
		logAgentReasoning:   logAgentReasoning,
		channelLogger:       channelLogger,
	}
}

// Process runs the tool-call loop for a single user message and returns the
// aggregated output string. The user message is appended to the session before
// processing begins.
func (a *Agent) Process(ctx context.Context, sess *session.Session, messageText, systemPrompt string) (string, error) {
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:    session.RoleUser,
		Content: messageText,
	})

	// Log user message to channel log
	_ = a.channelLogger.LogUser(sess.ChannelID, messageText)

	logger := log.GetGlobal().WithSource("agent")

	var output strings.Builder

	for i := 0; i < a.maxToolIterations; i++ {
		// Summarize context if needed to stay within context window
		if err := a.summarizeIfNeeded(ctx, sess, systemPrompt); err != nil {
			return output.String(), err
		}

		// Build messages for LLM request
		messages := a.toLLMMessages(sess, systemPrompt)

		// Serialize tool definitions for the request
		defs := a.tools.Definitions()
		var toolsJSON json.RawMessage
		if len(defs) > 0 {
			data, err := json.Marshal(defs)
			if err != nil {
				return "", fmt.Errorf("marshal tool definitions: %w", err)
			}
			toolsJSON = data
		}

		// Call LLM
		resp, err := a.client.Chat(ctx, messages, toolsJSON, a.maxTokens)
		if err != nil {
			logger.Error("LLM call failed", "error", err.Error())
			return output.String(), fmt.Errorf("LLM call failed (iteration %d): %w", i+1, err)
		}

		// Log and accumulate agent reasoning content
		if resp.ReasoningContent != "" {
			if a.logAgentReasoning {
				logger.Debug("agent reasoning", "content", resp.ReasoningContent)
			}
			output.WriteString("[Reasoning: " + resp.ReasoningContent + "]\n")
		}

		// Log and accumulate agent text content
		if resp.Content != "" {
			if a.logAgentReasoning {
				logger.Debug("agent response", "content", resp.Content)
			}
			output.WriteString(resp.Content)
		}

		// If no tool calls, record the final assistant message and we're done
		if len(resp.ToolCalls) == 0 {
			sess.Messages = append(sess.Messages, session.ConversationMessage{
				Role:             session.RoleAssistant,
				Content:          resp.Content,
				ReasoningContent: resp.ReasoningContent,
			})
			// Log final assistant message to channel log
			if resp.Content != "" {
				_ = a.channelLogger.LogAssistant(sess.ChannelID, resp.Content)
			}
			break
		}

		// Record assistant message with tool calls on session
		var sessTCs []session.ToolCall
		for _, tc := range resp.ToolCalls {
			stc := session.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
			}
			stc.Function.Name = tc.Function.Name
			stc.Function.Arguments = tc.Function.Arguments
			sessTCs = append(sessTCs, stc)
		}
		sess.Messages = append(sess.Messages, session.ConversationMessage{
			Role:             session.RoleAssistant,
			Content:          resp.Content,
			ReasoningContent: resp.ReasoningContent,
			ToolCalls:        sessTCs,
		})

		// Execute each tool call
		for _, tc := range resp.ToolCalls {
			if a.logToolCalls {
				logger.Debug("tool call", "tool", tc.Function.Name, "id", tc.ID,
					"arguments", tc.Function.Arguments)
			}

			output.WriteString(fmt.Sprintf("\n[Tool Call: %s]\n", tc.Function.Name))

			result, err := a.tools.Dispatch(tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				if a.logToolCalls {
					logger.Warn("tool error", "tool", tc.Function.Name, "error", err.Error())
				}
				result = err.Error()
			}

			if a.logToolCalls {
				logger.Debug("tool result", "tool", tc.Function.Name, "result", result)
			}

			// Log tool call to channel log
			_ = a.channelLogger.LogTool(sess.ChannelID, tc.Function.Name)

			output.WriteString(fmt.Sprintf("[Result: %s]\n", result))

			// Append tool result to session
			sess.Messages = append(sess.Messages, session.ConversationMessage{
				Role:       session.RoleTool,
				Content:    result,
				ToolCallID: tc.ID,
			})
		}

	}

	// If max iterations exhausted and last message is a tool result or
	// assistant message with tool calls (no final text), append a synthetic
	// closing message so session state is valid.
	lastMsg := sess.LastMessage()
	if lastMsg != nil && (lastMsg.Role == session.RoleTool || len(lastMsg.ToolCalls) > 0) {
		logger.Warn("max tool iterations reached — appended synthetic closing message")
		sess.Messages = append(sess.Messages, session.ConversationMessage{
			Role:    session.RoleAssistant,
			Content: "I reached my tool call limit this turn. Would you like me to continue?",
		})
		output.WriteString("\nI reached my tool call limit this turn. Would you like me to continue?")
	}

	return output.String(), nil
}

// summarizeIfNeeded checks whether context is approaching the limit and triggers
// summarization if so. Returns nil if no summarization was needed or it succeeded,
// or an error if summarization failed (caller should stop processing).
func (a *Agent) summarizeIfNeeded(ctx context.Context, sess *session.Session, systemPrompt string) error {
	if a.contextTokens <= 0 {
		return nil
	}

	totalTokens := a.totalTokens(sess, systemPrompt)
	limit := int(float64(a.contextTokens) * a.summarizeThreshold)

	if totalTokens <= limit {
		return nil
	}

	return a.summarizeContext(ctx, sess)
}

// summarizeContext compresses older messages in the session into a summary
// to stay within the context window. The most recent messages are preserved.
func (a *Agent) summarizeContext(ctx context.Context, sess *session.Session) error {
	logger := log.GetGlobal().WithSource("agent")

	// Log start
	logger.Info("context summarization started",
		"total_messages", fmt.Sprintf("%d", len(sess.Messages)),
		"keep_recent", fmt.Sprintf("%d", a.summarizeKeepRecent),
	)
	_ = a.channelLogger.Log(sess.ChannelID, channellog.Entry{
		Role:    "system",
		Action:  "tool",
		Tool:    "session_summary",
		Message: "Context summarization started",
	})

	// Split messages into old and recent
	old, recent := splitMessages(sess.Messages, a.summarizeKeepRecent)

	if len(old) == 0 {
		logger.Info("context summarization skipped (no old messages to summarize)")
		return nil
	}

	// Build messages for summary LLM call (no tools, just conversation)
	summaryMessages := make([]llm.Message, 0, len(old)+1)
	summaryMessages = append(summaryMessages, llm.Message{
		Role:    "system",
		Content: a.summaryPrompt,
	})
	for _, msg := range old {
		summaryMessages = append(summaryMessages, llm.Message{
			Role:             string(msg.Role),
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			ToolCallID:       msg.ToolCallID,
		})
	}

	// Call LLM for summary
	resp, err := a.client.Chat(ctx, summaryMessages, nil, a.maxTokens)
	if err != nil {
		errMsg := fmt.Sprintf("context summarization failed: %v", err)
		logger.Error(errMsg)
		_ = a.channelLogger.Log(sess.ChannelID, channellog.Entry{
			Role:    "system",
			Action:  "tool",
			Tool:    "session_summary",
			Message: errMsg,
		})
		// Record the failure in session state as a tool message
		sess.Messages = append(sess.Messages, session.ConversationMessage{
			Role:    session.RoleTool,
			Content: errMsg,
		})
		return fmt.Errorf("context summarization failed: %w", err)
	}

	summaryText := resp.Content
	if summaryText == "" {
		summaryText = resp.ReasoningContent
	}
	if summaryText == "" {
		errMsg := "context summarization failed: LLM returned empty summary"
		logger.Error(errMsg)
		_ = a.channelLogger.Log(sess.ChannelID, channellog.Entry{
			Role:    "system",
			Action:  "tool",
			Tool:    "session_summary",
			Message: errMsg,
		})
		sess.Messages = append(sess.Messages, session.ConversationMessage{
			Role:    session.RoleTool,
			Content: errMsg,
		})
		return fmt.Errorf("context summarization failed: LLM returned empty summary")
	}

	// Replace old messages with summary, keep recent
	sess.Messages = make([]session.ConversationMessage, 0, len(recent)+1)
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:             session.RoleAssistant,
		Content:          "",
		ReasoningContent: "[Summary of prior conversation]\n" + summaryText,
	})
	sess.Messages = append(sess.Messages, recent...)

	summaryTokens := len(summaryText) / 4
	logger.Info("context summarization complete",
		"old_messages", fmt.Sprintf("%d", len(old)),
		"kept_messages", fmt.Sprintf("%d", len(recent)),
		"summary_tokens", fmt.Sprintf("%d", summaryTokens),
	)
	_ = a.channelLogger.Log(sess.ChannelID, channellog.Entry{
		Role:    "system",
		Action:  "tool",
		Tool:    "session_summary",
		Message: fmt.Sprintf("Context summarization complete. Summarized %d messages, kept %d recent.", len(old), len(recent)),
	})

	return nil
}

// totalTokens estimates the total tokens in the system prompt plus all session messages.
func (a *Agent) totalTokens(sess *session.Session, systemPrompt string) int {
	total := len(systemPrompt) / 3
	for _, msg := range sess.Messages {
		total += len(msg.Content) / 3
		for _, tc := range msg.ToolCalls {
			total += len(tc.Function.Name) / 2
			total += len(tc.Function.Arguments) / 2
		}
		total += len(msg.ToolCallID) / 2
	}
	return total
}

// splitMessages splits the message list into old and recent groups.
// The most recent `keepRecent` messages are preserved; everything else is old.
func splitMessages(messages []session.ConversationMessage, keepRecent int) (old, recent []session.ConversationMessage) {
	if keepRecent <= 0 || len(messages) <= keepRecent {
		return messages, nil
	}
	return messages[:len(messages)-keepRecent], messages[len(messages)-keepRecent:]
}

// toLLMMessages converts session messages to LLM API messages, prepending the system prompt.
func (a *Agent) toLLMMessages(sess *session.Session, systemPrompt string) []llm.Message {
	msgs := make([]llm.Message, 0, len(sess.Messages)+1)

	// System prompt is always first
	msgs = append(msgs, llm.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	for _, msg := range sess.Messages {
		llmMsg := llm.Message{
			Role:             string(msg.Role),
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
		}

		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				llmTC := llm.ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
				}
				llmTC.Function.Name = tc.Function.Name
				llmTC.Function.Arguments = tc.Function.Arguments
				llmMsg.ToolCalls = append(llmMsg.ToolCalls, llmTC)
			}
		}

		if msg.ToolCallID != "" {
			llmMsg.ToolCallID = msg.ToolCallID
		}

		msgs = append(msgs, llmMsg)
	}

	return msgs
}
