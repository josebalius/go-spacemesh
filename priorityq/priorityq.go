package priorityq

import (
	"errors"
	"sync"
)

var (
	// ErrUnknownPriority indicates an incorrect attempt to make a write with an unknown priority
	ErrUnknownPriority = errors.New("unknown priority")

	// ErrQueueClosed indicates an attempt to read from a closed priority queue instance
	ErrQueueClosed = errors.New("the queue is closed")

	// ErrUnexpected indicates an unexpected error attempting to read from the queue
	ErrUnexpected = errors.New("unexpected error reading from queues")
)

// Priority is the type that indicates the priority of different queues
type Priority int

const (
	prioritiesCount = 3 // the number of priorities

	// High indicates the highest priority
	High = Priority(0)

	// Mid indicates the medium priority
	Mid = Priority(1)

	// Low indicates the lowest priority
	Low = Priority(2)
)

// Queue is the priority queue
type Queue struct {
	mu     sync.Mutex
	waitCh chan struct{}      // the channel that indicates if there is a message ready in the queue
	queues []chan interface{} // the queues where the index is the priority
}

// PriorityQueue is the interface used to interact with the queue
type PriorityQueue interface {
	Write(prio Priority, m interface{}) error
	Read() (interface{}, error)
	Close()
	Length() int
}

// New returns a new priority queue where each priority has a buffer of prioQueueLimit
func New(prioQueueLimit int) *Queue {
	// set queue for each priority
	qs := make([]chan interface{}, prioritiesCount)
	for i := range qs {
		qs[i] = make(chan interface{}, prioQueueLimit)
	}

	return &Queue{
		waitCh: make(chan struct{}, prioQueueLimit*prioritiesCount),
		queues: qs,
	}
}

// Write a message m to the associated queue with the provided priority
// This method blocks iff the queue is full
// Returns an error iff a queue does not exist for the provided name
// Note: writing to the pq after a call to close is forbidden and will result in a panic
func (pq *Queue) Write(prio Priority, m interface{}) error {
	if int(prio) >= cap(pq.queues) {
		return ErrUnknownPriority
	}

	pq.mu.Lock()
	pq.queues[prio] <- m
	pq.waitCh <- struct{}{}
	pq.mu.Unlock()

	return nil
}

// Read returns the next message by priority
// An error is returned iff the priority queue has been closed
func (pq *Queue) Read() (interface{}, error) {
	<-pq.waitCh // wait for m

	// pick by priority
	for _, q := range pq.queues {
		if q == nil { // if not set just continue
			continue
		}

		// if set, try read
		select {
		case m, ok := <-q:
			if !ok {
				// channels are starting to close
				return nil, ErrQueueClosed
			}
			return m, nil

		default: // empty, try next
			continue
		}
	}

	// should be unreachable
	return nil, ErrUnexpected
}

// Length returns the number of pending messages in all of the queues
func (pq *Queue) Length() (length int) {
	for _, q := range pq.queues {
		length += len(q)
	}
	return
}

// Close the priority queue
// No messages should be expected to be read after a call to Close
func (pq *Queue) Close() {
	for _, q := range pq.queues {
		if q != nil {
			pq.mu.Lock()
			close(q)
			pq.mu.Unlock()
		}
	}

	pq.mu.Lock()
	close(pq.waitCh)
	pq.mu.Unlock()
}
