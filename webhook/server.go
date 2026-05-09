package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/agent-project/harness/log"
	"github.com/agent-project/harness/queue"
	"github.com/agent-project/harness/session"
)

// Server handles inbound webhook HTTP requests.
type Server struct {
	host        string
	port        int
	webhookPath string
	q           *queue.Queue
	sessions    *session.Manager
	logEvents   bool

	server   *http.Server
	shutting atomic.Bool
}

// NewServer creates a new webhook HTTP server.
func NewServer(host string, port int, webhookPath string, q *queue.Queue, sessions *session.Manager, logChannelEvents bool) *Server {
	return &Server{
		host:        host,
		port:        port,
		webhookPath: webhookPath,
		q:           q,
		sessions:    sessions,
		logEvents:   logChannelEvents,
	}
}

// Start begins listening for HTTP requests. Call Stop() to shut down.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc(s.webhookPath, s.handleWebhook)

	s.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", s.host, s.port),
		Handler: mux,
	}

	logger := log.GetGlobal().WithSource("plugin.webhook")

	ln, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	logger.Info("webhook server listening", "addr", s.server.Addr)

	// Start serving in background
	go func() {
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Error("webhook server error", "error", err.Error())
		}
	}()

	// Block until context is done
	<-ctx.Done()
	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop() error {
	s.shutting.Store(true)
	if s.server != nil {
		return s.server.Shutdown(context.Background())
	}
	return nil
}

// handleWebhook processes inbound webhook POST requests.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	logger := log.GetGlobal().WithSource("plugin.webhook")

	// Return 503 if shutting down
	if s.shutting.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("service unavailable"))
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Channel     string `json:"channel"`
		Message     string `json:"message"`
		CallbackURL string `json:"callback_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid JSON"))
		return
	}

	if body.Channel == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("missing channel"))
		return
	}

	if body.Message == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("missing message"))
		return
	}

	// Ensure session exists for this channel (creates if new)
	s.sessions.Get(body.Channel)

	if s.logEvents {
		logger.Info("channel connected", "channel", body.Channel)
	}
	logger.Info("webhook message received", "channel", body.Channel)

	msg := queue.Message{
		ChannelID:   body.Channel,
		MessageText: fmt.Sprintf("[%s] [#%s] %s", time.Now().Format("01/02/2006 15:04:05"), body.Channel, body.Message),
		CallbackURL: body.CallbackURL,
	}

	rejection, _ := s.q.Enqueue(msg)
	if rejection != "" {
		// If a callback URL was provided, send the rejection notification there
		if body.CallbackURL != "" {
			_ = SendCallback(body.Channel, rejection, body.CallbackURL)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(rejection))
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("accepted"))
}
