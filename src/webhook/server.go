package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/agent-project/harness/log"
	"github.com/agent-project/harness/queue"
	"github.com/agent-project/harness/session"
)

// Server handles inbound webhook HTTP requests.
type Server struct {
	host         string
	port         int
	webhookPath  string
	maxBodyBytes int
	q            *queue.Queue
	sessions     *session.Manager
	logEvents    bool
	logger       *log.Logger

	server   *http.Server
	shutting atomic.Bool
}

// NewServer creates a new webhook HTTP server.
// logger may be nil (logging calls are no-ops).
func NewServer(host string, port int, webhookPath string, maxBodyBytes int, q *queue.Queue, sessions *session.Manager, logChannelEvents bool, logger *log.Logger) *Server {
	return &Server{
		host:         host,
		port:         port,
		webhookPath:  webhookPath,
		maxBodyBytes: maxBodyBytes,
		q:            q,
		sessions:     sessions,
		logEvents:    logChannelEvents,
		logger:       logger,
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

	logger := s.logger

	ln, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	if logger != nil {
		logger.Info("webhook server listening", "addr", s.server.Addr)
	}

	// Start serving in background
	go func() {
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			if logger != nil {
				logger.Error("webhook server error", "error", err.Error())
			}
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
	logger := s.logger

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

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, int64(s.maxBodyBytes))

	var body struct {
		Channel         string                   `json:"channel"`
		Message         string                   `json:"message"`
		CallbackURL     string                   `json:"callback_url"`
		ImageAttachment *session.ImageAttachment `json:"image_attachment"`
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

	if len(body.Channel) > 254 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("channel ID too long"))
		return
	}

	if body.Message == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("missing message"))
		return
	}

	if body.CallbackURL != "" {
		parsedURL, parseErr := url.Parse(body.CallbackURL)
		if parseErr != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("invalid callback_url (must be http or https)"))
			return
		}
	}

	// Ensure session exists for this channel (creates if new)
	s.sessions.Get(body.Channel)

	if s.logEvents && logger != nil {
		logger.Info("channel connected", "channel", body.Channel)
	}
	if logger != nil {
		logger.Info("webhook message received", "channel", body.Channel)
	}

	msg := queue.Message{
		ChannelID:   body.Channel,
		MessageText: fmt.Sprintf("[%s] [#%s] %s", time.Now().Format("01/02/2006 15:04:05"), body.Channel, body.Message),
		CallbackURL: body.CallbackURL,
	}
	if body.ImageAttachment != nil {
		msg.ImageAttachment = *body.ImageAttachment
	}

	rejection, _ := s.q.Enqueue(msg)
	if rejection != "" {
		// If a callback URL was provided, send the rejection notification there
		if body.CallbackURL != "" {
			_ = SendCallback(body.Channel, rejection, body.CallbackURL, s.logger)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(rejection))
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("accepted"))
}
