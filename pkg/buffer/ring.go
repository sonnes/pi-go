// Package buffer provides generic data structures for bounded storage.
package buffer

// Entry pairs a value with its monotonically increasing sequence number.
type Entry[T any] struct {
	Seq   int64
	Value T
}

// Ring is a fixed-capacity circular buffer that assigns a monotonically
// increasing sequence number to each written value. When full, the oldest
// entry is overwritten.
//
// Ring is not safe for concurrent use. Callers must synchronize access.
type Ring[T any] struct {
	entries []Entry[T]
	head    int
	count   int
	nextSeq int64
}

// NewRing creates a Ring with the given capacity.
// Capacity is clamped to a minimum of 1.
func NewRing[T any](capacity int) *Ring[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring[T]{
		entries: make([]Entry[T], capacity),
		nextSeq: 1,
	}
}

// Write appends a value to the ring buffer and returns its assigned
// sequence number. If the buffer is full, the oldest entry is overwritten.
func (r *Ring[T]) Write(value T) int64 {
	seq := r.nextSeq
	r.nextSeq++

	entry := Entry[T]{Seq: seq, Value: value}

	if r.count < len(r.entries) {
		r.entries[(r.head+r.count)%len(r.entries)] = entry
		r.count++
	} else {
		r.entries[r.head] = entry
		r.head = (r.head + 1) % len(r.entries)
	}

	return seq
}

// After returns all buffered entries with sequence numbers strictly
// greater than seq, ordered oldest to newest.
func (r *Ring[T]) After(seq int64) []Entry[T] {
	if r.count == 0 {
		return nil
	}

	// Find starting index using the oldest entry's sequence
	oldest := r.entries[r.head].Seq
	if seq < oldest {
		seq = oldest - 1
	}

	// Calculate how many entries to skip
	skip := int(seq - oldest + 1)
	if skip >= r.count {
		return nil
	}

	result := make([]Entry[T], r.count-skip)
	for i := range result {
		result[i] = r.entries[(r.head+skip+i)%len(r.entries)]
	}
	return result
}

// Len returns the number of entries currently stored.
func (r *Ring[T]) Len() int {
	return r.count
}

// Cap returns the maximum capacity of the ring buffer.
func (r *Ring[T]) Cap() int {
	return len(r.entries)
}

// Oldest returns the oldest buffered entry, or false if empty.
func (r *Ring[T]) Oldest() (Entry[T], bool) {
	if r.count == 0 {
		var zero Entry[T]
		return zero, false
	}
	return r.entries[r.head], true
}

// Newest returns the newest buffered entry, or false if empty.
func (r *Ring[T]) Newest() (Entry[T], bool) {
	if r.count == 0 {
		var zero Entry[T]
		return zero, false
	}
	idx := (r.head + r.count - 1) % len(r.entries)
	return r.entries[idx], true
}
