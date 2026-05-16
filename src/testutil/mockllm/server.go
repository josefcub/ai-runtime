package mockllm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ToolCall represents a tool invocation from the LLM.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// MockToolCall is a tool call configured on the mock server.
type MockToolCall struct {
	ID   string
	Name string
	Args string
}

// Server is a mock OpenAI-compatible /chat/completions SSE endpoint.
type Server struct {
	mu   sync.Mutex
	srv  *httptest.Server
	t    *testing.T
	mode string

	text       string
	reasoning  string
	tools      []MockToolCall
	errorMsg   string
	rawSSE     []string

	callCount  int
	lastBody   []byte
	lastModel  string
	lastTools  []json.RawMessage
	lastMaxTok int
}

// New creates and starts a mock LLM server. Call s.Close() when done.
func New(t *testing.T) (s *Server, baseURL string) {
	s = &Server{t: t}
	srv := httptest.NewServer(http.HandlerFunc(s.handle))
	s.srv = srv
	return s, srv.URL
}

// Close shuts down the mock server.
func (s *Server) Close() {
	if s.srv != nil {
		s.srv.CloseClientConnections()
		s.srv.Close()
	}
}

// SetResponseText configures a text-only streaming response.
func (s *Server) SetResponseText(text, reasoning string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = "text"
	s.text = text
	s.reasoning = reasoning
}

// SetResponseToolCalls configures a streaming tool-call response.
func (s *Server) SetResponseToolCalls(calls []MockToolCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = "tools"
	s.tools = calls
}

// SetError configures an HTTP 500 error response.
func (s *Server) SetError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = "error"
	s.errorMsg = msg
}

// InjectSSE configures raw SSE chunks to return verbatim.
func (s *Server) InjectSSE(chunks []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = "raw"
	s.rawSSE = chunks
}

// CallCount returns the number of requests received.
func (s *Server) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount
}

// LastBody returns the raw request body of the last request.
func (s *Server) LastBody() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := make([]byte, len(s.lastBody))
	copy(b, s.lastBody)
	return b
}

// LastModel returns the model string from the last request.
func (s *Server) LastModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastModel
}

// LastTools returns the tools array from the last request.
func (s *Server) LastTools() []json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]json.RawMessage, len(s.lastTools))
	copy(out, s.lastTools)
	return out
}

// LastMaxTokens returns the max_tokens value from the last request.
func (s *Server) LastMaxTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastMaxTok
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.callCount++
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	s.lastBody = body

	curMode := s.mode
	curText := s.text
	curReasoning := s.reasoning
	curTools := s.tools
	curError := s.errorMsg
	curRaw := s.rawSSE

	// Extract request fields
	var reqMap map[string]interface{}
	if err := json.Unmarshal(body, &reqMap); err == nil {
		if m, ok := reqMap["model"].(string); ok {
			s.lastModel = m
		}
		if tools, ok := reqMap["tools"].([]interface{}); ok {
			lastTools := make([]json.RawMessage, len(tools))
			for i, t := range tools {
				b, _ := json.Marshal(t)
				lastTools[i] = json.RawMessage(b)
			}
			s.lastTools = lastTools
		}
		if mt, ok := reqMap["max_tokens"].(float64); ok {
			s.lastMaxTok = int(mt)
		}
	}
	s.mu.Unlock()

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	switch curMode {
	case "error":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf(`{"error":{"message":"%s","type":"server_error"}}`, curError)))
		return

	case "raw":
		writeSSE(w, curRaw)
		return

	case "text":
		streamText(w, curText, curReasoning)
		return

	case "tools":
		streamToolCalls(w, curTools)
		return

	default:
		writeSSE(w, []string{
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		})
	}
}

func writeSSE(w http.ResponseWriter, chunks []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	for _, chunk := range chunks {
		fmt.Fprint(w, "data: ", chunk, "\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
	flushSSE(w, flusher)
}

func flushSSE(w http.ResponseWriter, flusher http.Flusher) {
	// No-op for writeSSE — it should not write a trailing empty data line
}

func flushSSEBody(w http.ResponseWriter, flusher http.Flusher, body string) {
	fmt.Fprint(w, "data: "+body+"\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func streamText(w http.ResponseWriter, text, reasoning string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	deltaText(w, flusher, `{"choices":[{"delta":{"role":"assistant"}}]}`)

	runes := []rune(text)
	idx := 0
	for idx < len(runes) {
		end := idx + 1
		if end > len(runes) {
			end = len(runes)
		}
		chunk := string(runes[idx:end])
		deltaText(w, flusher, fmt.Sprintf(`{"choices":[{"delta":{"content":"%s"}}]}`, escapeJSON(chunk)))
		idx = end
	}

	if reasoning != "" {
		runes = []rune(reasoning)
		idx = 0
		for idx < len(runes) {
			end := idx + 1
			if end > len(runes) {
				end = len(runes)
			}
			chunk := string(runes[idx:end])
			deltaText(w, flusher, fmt.Sprintf(`{"choices":[{"delta":{"reasoning_content":"%s"}}]}`, escapeJSON(chunk)))
			idx = end
		}
	}

	deltaText(w, flusher, `{"choices":[{"finish_reason":"stop"}]}`)
	flushSSE(w, flusher)
}

func deltaText(w http.ResponseWriter, flusher http.Flusher, jsonStr string) {
	fmt.Fprint(w, "data: ", jsonStr, "\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func streamToolCalls(w http.ResponseWriter, tools []MockToolCall) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	for i, tc := range tools {
		tcID := tc.ID
		if tcID == "" {
			tcID = fmt.Sprintf("call_%d", i)
		}
		deltaJSON(w, flusher, fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":%d,"id":"%s","type":"function"}]}}]}`, i, tcID))

		runes := []rune(tc.Name)
		for _, ch := range runes {
			deltaJSON(w, flusher, fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":%d,"function":{"name":"%c"}}]}}]}`, i, ch))
		}

		if tc.Args != "" {
			deltaJSON(w, flusher, fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":%d,"function":{"arguments":"%s"}}]}}]}`, i, escapeJSON(tc.Args)))
		}
	}

	deltaJSON(w, flusher, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`)
	flushSSE(w, flusher)
}

func deltaJSON(w http.ResponseWriter, flusher http.Flusher, jsonStr string) {
	fmt.Fprint(w, "data: ", jsonStr, "\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func parseSSE(raw []byte) []string {
	var chunks []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			chunks = append(chunks, strings.TrimPrefix(line, "data: "))
		}
	}
	return chunks
}

func toMap(data string) map[string]interface{} {
	var m map[string]interface{}
	json.Unmarshal([]byte(data), &m)
	return m
}
