package mockllm

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTextResponse(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	s.SetResponseText("Hello world", "")

	resp, err := http.Post(baseURL+"/chat/completions", "application/json", strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var foundStop bool
	var collected strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content       string `json:"content"`
					FinishReason *string `json:"finish_reason"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("parse chunk: %v", err)
		}
		if len(chunk.Choices) > 0 {
			if chunk.Choices[0].Delta.Content != "" {
				collected.WriteString(chunk.Choices[0].Delta.Content)
			}
			if chunk.Choices[0].Delta.FinishReason != nil && *chunk.Choices[0].Delta.FinishReason == "stop" {
				foundStop = true
			}
			if chunk.Choices[0].FinishReason != nil && *chunk.Choices[0].FinishReason == "stop" {
				foundStop = true
			}
		}
	}

	got := collected.String()
	if got != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", got)
	}
	if !foundStop {
		t.Error("expected finish_reason stop")
	}

	// Should have multiple chunks (not just one)
	if s.CallCount() != 1 {
		t.Errorf("expected 1 call, got %d", s.CallCount())
	}
	_ = time.Now() // ensure time import is used if needed
}

func TestTextWithReasoning(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	s.SetResponseText("answer", "thinking about it")

	body := strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	resp, err := http.Post(baseURL+"/chat/completions", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var content, reasoning strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content        string  `json:"content"`
					ReasoningContent *string `json:"reasoning_content"`
					FinishReason   *string `json:"finish_reason"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("parse chunk: %v", err)
		}
		if len(chunk.Choices) > 0 {
			if chunk.Choices[0].Delta.Content != "" {
				content.WriteString(chunk.Choices[0].Delta.Content)
			}
			if chunk.Choices[0].Delta.ReasoningContent != nil && *chunk.Choices[0].Delta.ReasoningContent != "" {
				reasoning.WriteString(*chunk.Choices[0].Delta.ReasoningContent)
			}
		}
	}

	if content.String() != "answer" {
		t.Errorf("expected content 'answer', got %q", content.String())
	}
	if reasoning.String() != "thinking about it" {
		t.Errorf("expected reasoning 'thinking about it', got %q", reasoning.String())
	}
}

func TestToolCallResponse(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	s.SetResponseToolCalls([]MockToolCall{
		{ID: "call_1", Name: "view", Args: `{"path":"foo.md"}`},
	})

	body := strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"view foo.md"}],"stream":true}`)
	resp, err := http.Post(baseURL+"/chat/completions", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var collectedToolCalls []map[string]interface{}
	var seenFinishReason bool

	buf := new(strings.Builder)
	_, err = io.Copy(buf, resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(buf.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("parse chunk: %v", err)
		}
		choices, ok := chunk["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]interface{})
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		if tcArr, ok := delta["tool_calls"].([]interface{}); ok {
			for _, tcVal := range tcArr {
				tc := tcVal.(map[string]interface{})
				collectedToolCalls = append(collectedToolCalls, tc)
			}
		}
		if fr, ok := choice["finish_reason"].(string); ok && fr == "stop" {
			seenFinishReason = true
		}
	}

	if len(collectedToolCalls) == 0 {
		t.Fatal("expected at least one tool call delta, got none")
	}

	if !seenFinishReason {
		t.Error("expected finish_reason stop")
	}
}

func TestErrorResponse(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	s.SetError("something broke")

	resp, err := http.Post(baseURL+"/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "something broke") {
		t.Errorf("expected error message in body, got %s", string(body))
	}
}

func TestRawSSEInjection(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	expected := []string{
		`{"choices":[{"delta":{"content":"hi"}}]}`,
	}
	s.InjectSSE(expected)

	resp, err := http.Post(baseURL+"/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	buf := new(strings.Builder)
	io.Copy(buf, resp.Body)
	raw := buf.String()

	if !strings.Contains(raw, `data: {"choices":[{"delta":{"content":"hi"}}]}`) {
		t.Errorf("expected raw chunk in response, got:\n%s", raw)
	}
}

func TestCallCountTracking(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	s.SetResponseText("ok", "")

	for i := 0; i < 3; i++ {
		_, err := http.Post(baseURL+"/chat/completions", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}

	if s.CallCount() != 3 {
		t.Errorf("expected 3 calls, got %d", s.CallCount())
	}
}

func TestLastRequestBodyCaptured(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	expectedBody := `{"model":"my-model","messages":[{"role":"user","content":"test"}],"stream":true}`
	s.SetResponseText("ok", "")

	_, err := http.Post(baseURL+"/chat/completions", "application/json", strings.NewReader(expectedBody))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	got := string(s.LastBody())
	if got != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, got)
	}
}

func TestDefaultResponse(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	// Don't configure any response — use default

	resp, err := http.Post(baseURL+"/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var seenStop bool
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("parse chunk: %v", err)
		}
		choices, _ := chunk["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]interface{})
		if fr, ok := choice["finish_reason"].(string); ok && fr == "stop" {
			seenStop = true
		}
		// Verify empty delta (no content, no tool_calls)
		delta, ok := choice["delta"].(map[string]interface{})
		if ok {
			if content := delta["content"]; content != nil {
				t.Errorf("expected empty content, got %v", content)
			}
			if tc := delta["tool_calls"]; tc != nil {
				t.Errorf("expected no tool_calls, got %v", tc)
			}
		}
	}

	if !seenStop {
		t.Error("expected finish_reason stop in default response")
	}
}

func TestNonPOSTReturns405(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	resp, err := http.Get(baseURL + "/chat/completions")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestMetricsLastMaxTokens(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	s.SetResponseText("ok", "")

	_, err := http.Post(baseURL+"/chat/completions", "application/json", strings.NewReader(`{"max_tokens":4096}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if s.LastMaxTokens() != 4096 {
		t.Errorf("expected lastMaxTokens 4096, got %d", s.LastMaxTokens())
	}
}

func TestDefaultResponse_NoConfiguration(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	resp, err := http.Post(baseURL+"/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	raw := string(body)

	if !strings.Contains(raw, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`) {
		t.Errorf("expected choices wrapper with delta in default response, got:\n%s", raw)
	}
	if !strings.Contains(raw, `"finish_reason":"stop"`) {
		t.Errorf("expected finish_reason stop in default response, got:\n%s", raw)
	}
}

func TestToolCallsMultiple(t *testing.T) {
	s, baseURL := New(t)
	defer s.Close()

	s.SetResponseToolCalls([]MockToolCall{
		{ID: "call_1", Name: "view", Args: `{"path":"foo.md"}`},
		{ID: "call_2", Name: "edit", Args: `{"path":"bar.md","diff":"old->new"}`},
	})

	body := strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"do both"}],"stream":true}`)
	resp, err := http.Post(baseURL+"/chat/completions", "application/json", body)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	buf := new(strings.Builder)
	_, err = io.Copy(buf, resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(buf.String()))
	var collectedToolCalls []map[string]interface{}
	var seenFinishReason bool
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("parse chunk: %v", err)
		}
		choices, ok := chunk["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]interface{})
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		if tcArr, ok := delta["tool_calls"].([]interface{}); ok {
			for _, tcVal := range tcArr {
				tc := tcVal.(map[string]interface{})
				collectedToolCalls = append(collectedToolCalls, tc)
			}
		}
		if fr, ok := choice["finish_reason"].(string); ok && fr == "stop" {
			seenFinishReason = true
		}
	}

	if len(collectedToolCalls) == 0 {
		t.Fatal("expected at least one tool call delta, got none")
	}

	indexToName := make(map[int]string)
	indexToArgs := make(map[int]string)
	for _, tc := range collectedToolCalls {
		if idx, ok := tc["index"].(float64); ok {
			if fn, ok := tc["function"].(map[string]interface{}); ok {
				if name, ok := fn["name"].(string); ok {
					indexToName[int(idx)] += name
				}
				if args, ok := fn["arguments"].(string); ok {
					indexToArgs[int(idx)] += args
				}
			}
		}
	}
	if indexToName[0] != "view" {
		t.Errorf("expected tool call[0] name 'view', got %q", indexToName[0])
	}
	if indexToName[1] != "edit" {
		t.Errorf("expected tool call[1] name 'edit', got %q", indexToName[1])
	}

	if !seenFinishReason {
		t.Error("expected finish_reason stop")
	}
}
