package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/agent-project/harness/config"
	"github.com/agent-project/harness/imageutil"
	"github.com/agent-project/harness/session"
)

// ---------- CLI Entry Point ----------

func main() {
	cfgPath := flag.String("c", "./config.ini", "Path to config.ini")
	noCallback := flag.Bool("nc", false, "Fire-and-forget (no callback wait)")
	cbURL := flag.String("cb", "", "Callback URL to use instead of local server")
	channel := flag.String("n", "cli", "Harness channel ID")
	trace := flag.Bool("t", false, "Show reasoning and tool calls in output (auto-enabled when log level is debug)")
	imageFile := flag.String("v", "", "Image file path to attach to the message")
	flag.Parse()

	// Resolve callback mode (default: spin up local server and wait)
	callbackMode := "local"
	if *noCallback {
		if *cbURL != "" {
			fmt.Fprintln(os.Stderr, "error: -cb and -nc are mutually exclusive")
			os.Exit(1)
		}
		callbackMode = "none"
	}
	if *cbURL != "" {
		resolved, err := url.Parse(*cbURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid callback URL: %v\n", err)
			os.Exit(1)
		}
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			fmt.Fprintln(os.Stderr, "error: callback URL must use http or https")
			os.Exit(1)
		}
		callbackMode = "external"
	}

	message := flag.Arg(0)
	if message == "" {
		fmt.Fprintln(os.Stderr, "error: message is required")
		fmt.Fprintln(os.Stderr, "usage: client [options] <message>")
		flag.Usage()
		os.Exit(1)
	}

	// Validate and encode the image file if provided
	var imageAtt session.ImageAttachment
	if *imageFile != "" {
		data, err := os.ReadFile(*imageFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading image: %v\n", err)
			os.Exit(1)
		}
		if len(data) > imageutil.MaxImageSize {
			fmt.Fprintf(os.Stderr, "error: image too large: %d bytes (max %d)\n", len(data), imageutil.MaxImageSize)
			os.Exit(1)
		}
		mime := imageutil.DetectMIME(data)
		if mime == "" {
			fmt.Fprintf(os.Stderr, "error: not a recognized image file: %s\n", *imageFile)
			os.Exit(1)
		}
		imageAtt = session.ImageAttachment{
			Data:     base64.StdEncoding.EncodeToString(data),
			MIMEType: mime,
		}
	}

	// Load just the [server] and [logging] sections from the shared config.ini
	data, err := config.ParseFile(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	// Auto-enable trace mode when log level is 'debug'
	if !*trace && strings.ToLower(strVal(data, "logging", "level", "")) == "debug" {
		*trace = true
	}

	srvHost := strVal(data, "server", "host", "127.0.0.1")
	srvPort := intVal(data, "server", "port", 8080)
	webhookPath := strVal(data, "server", "webhook_path", "/webhook")
	client := &httpClient{
		host:        srvHost,
		port:        srvPort,
		webhookPath: webhookPath,
	}

	if callbackMode == "none" {
		err = client.Send(*channel, message, "", imageAtt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error sending message: %v\n", err)
			os.Exit(1)
		}
		if *trace {
			fmt.Println("Message sent. (no callback)")
		}
		os.Exit(0)
	}

	if callbackMode == "external" {
		err = client.Send(*channel, message, *cbURL, imageAtt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error sending message: %v\n", err)
			os.Exit(1)
		}
		if *trace {
			fmt.Printf("Message sent (callback to %s).\n", *cbURL)
		}
		os.Exit(0)
	}

	// Start a local callback server
	cb, err := newCallbackServer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error starting callback server: %v\n", err)
		os.Exit(1)
	}

	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback", cb.port)

	// Handle interrupts
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cb.stop()
		os.Exit(130)
	}()

	err = client.Send(*channel, message, callbackURL, imageAtt)
	if err != nil {
		cb.stop()
		fmt.Fprintf(os.Stderr, "error sending message: %v\n", err)
		os.Exit(1)
	}

	if *trace {
		fmt.Printf("Message sent to channel %q. Waiting for callback on %s...\n\n", *channel, callbackURL)
	}

	result := <-cb.ch
	cb.stop()

	output := result.Message
	if !*trace {
		output = stripTraceOutput(output)
	}
	fmt.Println(output + "\n")
}

// ---------- HTTP Client ----------

type httpClient struct {
	host        string
	port        int
	webhookPath string
}

// WebhookRequest matches the harness inbound JSON schema.
type WebhookRequest struct {
	Channel        string                    `json:"channel"`
	Message        string                    `json:"message"`
	CallbackURL    string                    `json:"callback_url,omitempty"`
	ImageAttachment *session.ImageAttachment `json:"image_attachment,omitempty"`
}

// CallbackResponse matches the harness outbound JSON schema.
type CallbackResponse struct {
	Channel string `json:"channel"`
	Message string `json:"message"`
}

// Send posts a webhook message to the harness.
func (c *httpClient) Send(channel, message, callbackURL string, imageAtt session.ImageAttachment) error {
	payload := WebhookRequest{
		Channel: channel,
		Message: message,
	}
	if callbackURL != "" {
		payload.CallbackURL = callbackURL
	}
	if imageAtt.Data != "" {
		payload.ImageAttachment = &imageAtt
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("http://%s:%d%s", c.host, c.port, c.webhookPath)

	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("harness returned status %d", resp.StatusCode)
	}

	return nil
}

// ---------- Callback Server ----------

type callbackServer struct {
	port int
	srv  *http.Server
	ch   chan CallbackResponse
}

func newCallbackServer() (*callbackServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	port := ln.Addr().(*net.TCPAddr).Port

	s := &callbackServer{
		port: port,
		ch:   make(chan CallbackResponse, 1),
	}

	s.srv = &http.Server{Handler: s}

	go func() {
		_ = s.srv.Serve(ln)
	}()

	return s, nil
}

func (s *callbackServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var resp CallbackResponse
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Tell the harness to close its side of the connection after this response.
	// This prevents an EOF error on the harness when the client exits immediately
	// after receiving the callback.
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusOK)
	select {
	case s.ch <- resp:
	default:
	}
}

func (s *callbackServer) stop() {
	if s.srv != nil {
		s.srv.Close()
	}
}

// ---------- Minimal INI Helpers ----------

func strVal(data map[string]map[string]string, section, key, defaultValue string) string {
	if sec, ok := data[section]; ok {
		if val, ok := sec[key]; ok {
			return val
		}
	}
	return defaultValue
}

func intVal(data map[string]map[string]string, section, key string, defaultValue int) int {
	if sec, ok := data[section]; ok {
		if raw, ok := sec[key]; ok {
			val, err := strconv.Atoi(strings.TrimSpace(raw))
			if err == nil {
				return val
			}
		}
	}
	return defaultValue
}

// ---------- Output Filtering ----------

// stripTraceOutput removes [Reasoning: ...], [Tool Call: ...], and [Result: ...] blocks
// from the LLM response, leaving only the final text content.
func stripTraceOutput(message string) string {
	// Remove [Reasoning: ...] blocks (single-line)
	result := message
	for {
		start := strings.Index(result, "[Reasoning: ")
		if start == -1 {
			break
		}
		end := strings.IndexByte(result[start:], ']')
		if end == -1 {
			break
		}
		end = start + end + 1
		result = result[:start] + result[end:]
	}

	// Remove [Tool Call: ...] blocks
	result = removeBlocks(result, "[Tool Call: ", "]")

	// Remove [Result: ...] blocks
	result = removeBlocks(result, "[Result: ", "]")

	// Clean up excess blank lines (collapse 2+ consecutive newlines into 2)
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	// Trim leading/trailing whitespace
	result = strings.TrimSpace(result)
	if result == "" {
		return message
	}
	return result + "\n"
}

// removeBlocks removes all blocks that start with openMarker and end with closeMarker,
// including surrounding newlines.
func removeBlocks(s, openMarker, closeMarker string) string {
	for {
		start := strings.Index(s, openMarker)
		if start == -1 {
			break
		}
		// Strip leading newline if present
		if start > 0 && s[start-1] == '\n' {
			start--
		}
		end := strings.Index(s[start:], closeMarker)
		if end == -1 {
			break
		}
		end = start + end + len(closeMarker)
		// Strip trailing newline(s) if present
		for end < len(s) && s[end] == '\n' {
			end++
		}
		s = s[:start] + s[end:]
	}
	return s
}
