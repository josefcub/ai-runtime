package queue

import (
	"fmt"
	"sync"
	"time"

	"github.com/agent-project/harness/log"
)

const rejectionMessage = "Queue full. Messages are being dropped. Please wait and retry."

// Message represents an inbound message from a channel.
type Message struct {
	ChannelID   string
	MessageText string
	CallbackURL string
	ArrivalTime time.Time
}

// Queue is a FIFO message queue with per-channel depth tracking and backpressure.
type Queue struct {
	mu       sync.Mutex
	items    []Message
	maxDepth int
	// depth tracks the number of pending messages per channel
	depth map[string]int
}

// New creates a new Queue with the given per-channel max depth.
func New(maxDepth int) *Queue {
	return &Queue{
		maxDepth: maxDepth,
		depth:    make(map[string]int),
	}
}

// Enqueue adds a message to the queue. Returns (nil, "") on success, or
// (nil, rejectionMessage) when the channel's depth has reached maxDepth.
func (q *Queue) Enqueue(msg Message) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	msg.ArrivalTime = time.Now().UTC()

	if q.depth[msg.ChannelID] >= q.maxDepth {
		log.GetGlobal().Warn("queue full — message rejected",
			"channel", msg.ChannelID,
			"depth", fmt.Sprintf("%d", q.depth[msg.ChannelID]),
			"max_depth", fmt.Sprintf("%d", q.maxDepth),
		)
		return rejectionMessage, nil
	}

	q.items = append(q.items, msg)
	q.depth[msg.ChannelID]++

	return "", nil
}

// Dequeue removes and returns the oldest message. Returns the message and true
// on success, or a zero Message and false when the queue is empty.
func (q *Queue) Dequeue() (Message, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return Message{}, false
	}

	msg := q.items[0]
	q.items = q.items[1:]
	q.depth[msg.ChannelID]--

	return msg, true
}

// Depth returns the current number of pending messages for the given channel.
func (q *Queue) Depth(channelID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.depth[channelID]
}

// Len returns the total number of messages in the queue.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Pending returns a snapshot of all pending messages (for graceful shutdown).
func (q *Queue) Pending() []Message {
	q.mu.Lock()
	defer q.mu.Unlock()

	snap := make([]Message, len(q.items))
	copy(snap, q.items)
	return snap
}

// Clear removes all pending messages and resets per-channel depth counters.
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = nil
	q.depth = make(map[string]int)
}
