package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/agent-project/harness/agent"
	"github.com/agent-project/harness/channellog"
	"github.com/agent-project/harness/config"
	"github.com/agent-project/harness/llm"
	"github.com/agent-project/harness/log"
	"github.com/agent-project/harness/queue"
	"github.com/agent-project/harness/sandbox"
	"github.com/agent-project/harness/session"
	"github.com/agent-project/harness/tools"
	"github.com/agent-project/harness/webhook"
	"github.com/agent-project/harness/worker"
)

func main() {
	configPath := flag.String("config", "./config.ini", "Path to the INI configuration file")
	flag.Parse()

	// 1. Load and validate config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to load config: %v\n", err)
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	// 2. Initialize logger before any other subsystem
	logger, err := log.New(cfg.Paths.LogDir, log.ParseLevel(cfg.Logging.Level))
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Close()

	log.SetGlobal(logger)

	// 3. Resolve working directory for sandbox
	workingDir, err := sandbox.ResolveWorkingDir(cfg.Paths.WorkingDir)
	if err != nil {
		logger.Error("failed to resolve working directory", "error", err.Error())
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	// 4. Create message queue
	q := queue.New(cfg.Queue.MaxDepth)

	// 5. Create session manager and load existing sessions
	sessions := session.NewManager(cfg.Paths.StateDir)
	if err := sessions.LoadAll(); err != nil {
		logger.Warn("failed to load existing sessions", "error", err.Error())
	}

	// 6. Create tool registry and register built-in tools
	reg := tools.New(workingDir)
	tools.RegisterFileTools(reg)
	tools.RegisterWebTools(reg)
	tools.RegisterBashTools(reg, cfg.Bash.Enabled, cfg.Bash.Timeout, cfg.Bash.MaxOutput, cfg.Bash.Banned)

	// 7. Create LLM client
	llmClient := llm.New(cfg.LLM.Endpoint, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout, cfg.Paths.LogDir)

	// 8. Create channel conversation logger
	channelLogger := channellog.New(cfg.Paths.ChannelLogDir)

	// 9. Create agent
	agt := agent.New(
		llmClient,
		reg,
		cfg.LLM.MaxToolIterations,
		cfg.LLM.ContextTokens,
		cfg.LLM.SummarizeThreshold,
		cfg.LLM.SummarizeKeepRecent,
		cfg.LLM.MaxTokens,
		session.SummaryPrompt,
		cfg.Logging.LogToolCalls,
		cfg.Logging.LogAgentReasoning,
		channelLogger,
	)

	// 10. Create webhook server
	ws := webhook.NewServer(
		cfg.Server.Host,
		cfg.Server.Port,
		cfg.Server.WebhookPath,
		q,
		sessions,
		cfg.Logging.LogChannelEvents,
	)

	// 11. Create worker
	wrk := worker.New(q, sessions, agt, cfg.LLM.SystemPrompt, workingDir)

	// 12. Create cancellable context for shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 13. Start webhook server (blocks until ctx is cancelled)
	go func() {
		if err := ws.Start(ctx); err != nil {
			logger.Error("webhook server error", "error", err.Error())
		}
	}()

	// 14. Start worker (blocks until ctx is cancelled)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wrk.Run(ctx)
	}()

	logger.Info("agent harness starting",
		"endpoint", cfg.LLM.Endpoint,
		"model", cfg.LLM.Model,
		"host", cfg.Server.Host,
		"port", fmt.Sprintf("%d", cfg.Server.Port),
		"webhook_path", cfg.Server.WebhookPath,
		"log_level", cfg.Logging.Level,
	)

	// 15. Wait for termination signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigCh
	logger.Info("received signal, initiating graceful shutdown", "signal", sig.String())

	// === Graceful Shutdown ===

	// Step 1: Stop accepting new webhook messages (return 503)
	logger.Info("stopping webhook server")
	if err := ws.Stop(); err != nil {
		logger.Error("webhook server stop error", "error", err.Error())
	}

	// Step 2: Cancel context to stop the worker
	cancel()

	// Wait for worker to finish its current message (if any)
	wg.Wait()

	// Step 3: Drain the message queue — append pending messages to session state files.
	// Do not call the LLM; this is solely to prevent message loss.
	pending := q.Pending()
	for _, msg := range pending {
		if err := sessions.DrainAndSave(msg.ChannelID, msg.MessageText); err != nil {
			logger.Error("failed to drain message to session",
				"channel", msg.ChannelID,
				"error", err.Error(),
			)
		} else {
			logger.Info("drained pending message to session", "channel", msg.ChannelID)
		}
	}

	// Step 4: Flush all session state to disk atomically
	if err := sessions.SaveAll(); err != nil {
		logger.Error("failed to save all sessions", "error", err.Error())
	} else {
		logger.Info("all sessions flushed to disk")
	}

	// Step 5: Clear the queue (messages have been drained)
	q.Clear()

	logger.Info("agent harness shutting down")
}
