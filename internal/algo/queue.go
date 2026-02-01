package algo

import (
	"sync"
	"time"
)

// SignalQueue holds trade signals and flushes them every minute
type SignalQueue struct {
	mu            sync.Mutex
	signals       []TradeSignal
	processedSet  map[uint32]struct{} // tracks tokens already processed this minute
	currentMinute int
}

func NewSignalQueue() *SignalQueue {
	now := time.Now()
	return &SignalQueue{
		signals:       make([]TradeSignal, 0),
		processedSet:  make(map[uint32]struct{}),
		currentMinute: now.Hour()*60 + now.Minute(),
	}
}

// Push adds a signal to the queue if the token hasn't been processed this minute
// Returns true if signal was added, false if already processed this minute
func (sq *SignalQueue) Push(signal TradeSignal) bool {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	// Check and update minute boundary
	sq.checkMinuteBoundary()

	// Check if this token was already processed this minute
	if _, exists := sq.processedSet[signal.InstrumentToken]; exists {
		return false
	}

	// Mark as processed for this minute
	sq.processedSet[signal.InstrumentToken] = struct{}{}
	sq.signals = append(sq.signals, signal)
	return true
}

// Flush returns all signals and clears the queue
// This should be called at the start of each minute
func (sq *SignalQueue) Flush() []TradeSignal {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	signals := sq.signals
	sq.signals = make([]TradeSignal, 0)

	// Reset the processed set for the new minute
	sq.processedSet = make(map[uint32]struct{})
	sq.currentMinute = getCurrentMinute()

	return signals
}

// Peek returns current signals without removing them
func (sq *SignalQueue) Peek() []TradeSignal {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	result := make([]TradeSignal, len(sq.signals))
	copy(result, sq.signals)
	return result
}

// Len returns the current number of signals in the queue
func (sq *SignalQueue) Len() int {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	return len(sq.signals)
}

// IsTokenProcessedThisMinute checks if a token has already been processed
func (sq *SignalQueue) IsTokenProcessedThisMinute(token uint32) bool {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	sq.checkMinuteBoundary()
	_, exists := sq.processedSet[token]
	return exists
}

// checkMinuteBoundary resets the processed set if minute has changed
// Must be called with lock held
func (sq *SignalQueue) checkMinuteBoundary() {
	currentMin := getCurrentMinute()
	if currentMin != sq.currentMinute {
		sq.processedSet = make(map[uint32]struct{})
		sq.currentMinute = currentMin
	}
}

func getCurrentMinute() int {
	now := time.Now()
	return now.Hour()*60 + now.Minute()
}
