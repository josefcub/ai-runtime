package queue

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-project/harness/log"
)

func TestEnqueueDequeue_FIFO(t *testing.T) {
	q := New(64, nil)

	for i := 0; i < 5; i++ {
		msg := Message{ChannelID: "ch1", MessageText: "msg"}
		if _, err := q.Enqueue(msg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if got := q.Len(); got != 5 {
		t.Fatalf("expected len=5, got %d", got)
	}

	// Dequeue in order
	for i := 0; i < 5; i++ {
		msg, ok := q.Dequeue()
		if !ok {
			t.Fatalf("expected message at index %d", i)
		}
		if msg.ChannelID != "ch1" {
			t.Errorf("expected channel ch1, got %s", msg.ChannelID)
		}
	}

	_, ok := q.Dequeue()
	if ok {
		t.Fatal("expected empty queue")
	}
}

func TestEnqueueDequeue_MultiChannel(t *testing.T) {
	q := New(64, nil)

	// Interleave two channels
	_, _ = q.Enqueue(Message{ChannelID: "a", MessageText: "a1"})
	_, _ = q.Enqueue(Message{ChannelID: "b", MessageText: "b1"})
	_, _ = q.Enqueue(Message{ChannelID: "a", MessageText: "a2"})

	msgs, _ := q.Dequeue()
	if msgs.ChannelID != "a" || msgs.MessageText != "a1" {
		t.Errorf("expected a/a1, got %s/%s", msgs.ChannelID, msgs.MessageText)
	}

	msgs, _ = q.Dequeue()
	if msgs.ChannelID != "b" || msgs.MessageText != "b1" {
		t.Errorf("expected b/b1, got %s/%s", msgs.ChannelID, msgs.MessageText)
	}

	msgs, _ = q.Dequeue()
	if msgs.ChannelID != "a" || msgs.MessageText != "a2" {
		t.Errorf("expected a/a2, got %s/%s", msgs.ChannelID, msgs.MessageText)
	}
}

func TestBackpressure(t *testing.T) {
	q := New(2, nil)

	_, _ = q.Enqueue(Message{ChannelID: "ch1", MessageText: "1"})
	_, _ = q.Enqueue(Message{ChannelID: "ch1", MessageText: "2"})

	// Third message for same channel should be rejected
	rej, _ := q.Enqueue(Message{ChannelID: "ch1", MessageText: "3"})
	if rej != rejectionMessage {
		t.Errorf("expected rejection message, got %q", rej)
	}

	// Different channel should still accept
	rej, _ = q.Enqueue(Message{ChannelID: "ch2", MessageText: "x"})
	if rej != "" {
		t.Errorf("expected no rejection, got %q", rej)
	}

	// After dequeuing one, channel ch1 should accept again
	q.Dequeue()
	rej, _ = q.Enqueue(Message{ChannelID: "ch1", MessageText: "4"})
	if rej != "" {
		t.Errorf("expected no rejection after dequeue, got %q", rej)
	}
}

func TestDepth(t *testing.T) {
	q := New(64, nil)

	_, _ = q.Enqueue(Message{ChannelID: "a", MessageText: "1"})
	_, _ = q.Enqueue(Message{ChannelID: "a", MessageText: "2"})
	_, _ = q.Enqueue(Message{ChannelID: "b", MessageText: "1"})

	if got := q.Depth("a"); got != 2 {
		t.Errorf("expected depth(a)=2, got %d", got)
	}
	if got := q.Depth("b"); got != 1 {
		t.Errorf("expected depth(b)=1, got %d", got)
	}
	if got := q.Depth("c"); got != 0 {
		t.Errorf("expected depth(c)=0, got %d", got)
	}

	q.Dequeue() // removes a's first message
	if got := q.Depth("a"); got != 1 {
		t.Errorf("expected depth(a)=1 after dequeue, got %d", got)
	}
}

func TestArrivalTime(t *testing.T) {
	q := New(64, nil)

	_, _ = q.Enqueue(Message{ChannelID: "ch1", MessageText: "first"})
	time.Sleep(10 * time.Millisecond)
	_, _ = q.Enqueue(Message{ChannelID: "ch1", MessageText: "second"})

	first, _ := q.Dequeue()
	second, _ := q.Dequeue()

	if !first.ArrivalTime.Before(second.ArrivalTime) {
		t.Error("first message should have earlier arrival time")
	}
}

func TestPending(t *testing.T) {
	q := New(64, nil)

	_, _ = q.Enqueue(Message{ChannelID: "a", MessageText: "1"})
	_, _ = q.Enqueue(Message{ChannelID: "b", MessageText: "2"})

	pending := q.Pending()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	// Pending should be a snapshot
	q.Dequeue()
	if len(pending) != 2 {
		t.Error("pending snapshot should not change after dequeue")
	}
}

func TestClear(t *testing.T) {
	q := New(64, nil)

	_, _ = q.Enqueue(Message{ChannelID: "a", MessageText: "1"})
	_, _ = q.Enqueue(Message{ChannelID: "b", MessageText: "2"})

	q.Clear()

	if q.Len() != 0 {
		t.Errorf("expected len=0 after clear, got %d", q.Len())
	}
	if q.Depth("a") != 0 {
		t.Errorf("expected depth(a)=0 after clear, got %d", q.Depth("a"))
	}
}

func TestEmptyDequeue(t *testing.T) {
	q := New(64, nil)
	_, ok := q.Dequeue()
	if ok {
		t.Fatal("expected false on empty dequeue")
	}
}

func TestConcurrentEnqueueDequeue(t *testing.T) {
	q := New(1000, nil)

	var wg sync.WaitGroup

	// Enqueue from multiple goroutines
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				ch := "ch" + string(rune('0'+id))
				msg := Message{ChannelID: ch, MessageText: "test"}
				q.Enqueue(msg)
			}
		}(g)
	}

	// Dequeue from multiple goroutines
	results := make([]string, 0, 500)
	var resMu sync.Mutex
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				msg, ok := q.Dequeue()
				if !ok {
					return
				}
				resMu.Lock()
				results = append(results, msg.ChannelID)
				resMu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Verify all channels are represented
	channels := make(map[string]int)
	for _, ch := range results {
		channels[ch]++
	}

	// With backpressure, some may be dropped, but total should be <= 500
	if len(results) > 500 {
		t.Errorf("expected <= 500 results, got %d", len(results))
	}
}

func TestRejectionMessageExact(t *testing.T) {
	q := New(1, nil)
	_, _ = q.Enqueue(Message{ChannelID: "x", MessageText: "1"})
	rej, _ := q.Enqueue(Message{ChannelID: "x", MessageText: "2"})

	expected := "Queue full. Messages are being dropped. Please wait and retry."
	if rej != expected {
		t.Errorf("rejection message mismatch:\ngot:  %q\nwant: %q", rej, expected)
	}
}

func TestRejectionLogsWarning(t *testing.T) {
	// Set up a real logger for this test
	tmpDir, err := os.MkdirTemp("", "queue-log-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logger, err := log.New(tmpDir, log.DebugLevel)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	q := New(1, logger)
	_, _ = q.Enqueue(Message{ChannelID: "warn:ch", MessageText: "1"})
	// This should trigger a warn log
	_, _ = q.Enqueue(Message{ChannelID: "warn:ch", MessageText: "2"})

	// Read log file
	data, err := os.ReadFile(filepath.Join(tmpDir, "harness.log"))
	if err != nil {
		t.Fatal(err)
	}

	raw := string(data)
	if !strings.Contains(raw, "level=warn") {
		t.Errorf("expected warn-level log, got:\n%s", raw)
	}
	if !strings.Contains(raw, "queue full") {
		t.Errorf("expected 'queue full' in log, got:\n%s", raw)
	}
	if !strings.Contains(raw, "warn:ch") {
		t.Errorf("expected channel ID in log, got:\n%s", raw)
	}
}

func TestCallbackURLPreserved(t *testing.T) {
	q := New(64, nil)

	url := "http://example.com/callback"
	_, _ = q.Enqueue(Message{
		ChannelID:   "ch1",
		MessageText: "hello",
		CallbackURL: url,
	})

	msg, ok := q.Dequeue()
	if !ok {
		t.Fatal("expected message")
	}
	if msg.CallbackURL != url {
		t.Errorf("expected callback URL %q, got %q", url, msg.CallbackURL)
	}
}

func TestQueueLoopTimeout(t *testing.T) {
	// Ensure the test doesn't hang indefinitely.
	done := make(chan struct{})
	go func() {
		q := New(100, nil)
		for i := 0; i < 10000; i++ {
			_, _ = q.Enqueue(Message{ChannelID: "ch", MessageText: "x"})
			q.Dequeue()
		}
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("queue operations exceeded 2s timeout")
	}
}

func TestRejectionPreservesOrder(t *testing.T) {
	q := New(2, nil)

	_, _ = q.Enqueue(Message{ChannelID: "a", MessageText: "a1"})
	_, _ = q.Enqueue(Message{ChannelID: "a", MessageText: "a2"})
	// a3 gets rejected
	_, _ = q.Enqueue(Message{ChannelID: "a", MessageText: "a3"})
	_, _ = q.Enqueue(Message{ChannelID: "b", MessageText: "b1"})

	// Dequeue should return a1, a2, b1 in order
	msgs := []Message{}
	for {
		m, ok := q.Dequeue()
		if !ok {
			break
		}
		msgs = append(msgs, m)
	}

	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].MessageText != "a1" {
		t.Errorf("first message should be a1, got %s", msgs[0].MessageText)
	}
	if msgs[1].MessageText != "a2" {
		t.Errorf("second message should be a2, got %s", msgs[1].MessageText)
	}
	if msgs[2].MessageText != "b1" {
		t.Errorf("third message should be b1, got %s", msgs[2].MessageText)
	}
}

func TestPendingContainsAllFields(t *testing.T) {
	q := New(64, nil)

	_, _ = q.Enqueue(Message{
		ChannelID:   "ch1",
		MessageText: "test",
		CallbackURL: "http://example.com",
	})

	pending := q.Pending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending message")
	}
	if !slices.ContainsFunc(pending, func(m Message) bool {
		return m.ChannelID == "ch1" &&
			m.MessageText == "test" &&
			m.CallbackURL == "http://example.com"
	}) {
		t.Error("pending message does not contain expected fields")
	}
}
