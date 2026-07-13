package state

import (
	"sync"
	"time"

	"agent-beacon/internal/protocol"
)

type EventResult string

const (
	EventAccepted  EventResult = "accepted"
	EventDuplicate EventResult = "deduplicated"
	EventExpired   EventResult = "expired"
)

type ACKRecord struct {
	ACK      protocol.ACK `json:"ack"`
	Received time.Time    `json:"received_at"`
}

type acceptedIndex struct {
	id         string
	dedupeKey  string
	acceptedAt time.Time
}

type Store struct {
	mu           sync.RWMutex
	dedupeWindow time.Duration
	capacity     int
	events       []protocol.Envelope
	acks         []ACKRecord
	seenIDs      map[string]struct{}
	dedupeKeys   map[string]time.Time
	recent       []acceptedIndex
	revision     uint64
}

func NewStore(dedupeWindow time.Duration, capacity int) *Store {
	if capacity < 1 {
		capacity = 1
	}
	return &Store{
		dedupeWindow: dedupeWindow,
		capacity:     capacity,
		seenIDs:      make(map[string]struct{}),
		dedupeKeys:   make(map[string]time.Time),
	}
}

func (store *Store) AddEvent(envelope protocol.Envelope, now time.Time) EventResult {
	result, _ := store.AcceptEvent(envelope, now)
	return result
}

func (store *Store) AcceptEvent(envelope protocol.Envelope, now time.Time) (EventResult, protocol.Envelope) {
	notification, err := protocol.DecodePayload[protocol.Notification](envelope)
	if err != nil {
		return EventExpired, protocol.Envelope{}
	}
	if !notification.ExpiresAt.IsZero() && !notification.ExpiresAt.After(now) {
		return EventExpired, protocol.Envelope{}
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.seenIDs[envelope.ID]; exists {
		return EventDuplicate, protocol.Envelope{}
	}
	if acceptedAt, exists := store.dedupeKeys[notification.DedupeKey]; exists &&
		store.dedupeWindow > 0 && now.Sub(acceptedAt) >= 0 && now.Sub(acceptedAt) < store.dedupeWindow {
		return EventDuplicate, protocol.Envelope{}
	}
	store.revision++
	envelope.Revision = store.revision
	store.seenIDs[envelope.ID] = struct{}{}
	store.dedupeKeys[notification.DedupeKey] = now
	store.recent = append(store.recent, acceptedIndex{
		id: envelope.ID, dedupeKey: notification.DedupeKey, acceptedAt: now,
	})
	store.events = append(store.events, envelope)
	if len(store.events) > store.capacity {
		store.events = append([]protocol.Envelope(nil), store.events[len(store.events)-store.capacity:]...)
	}
	if len(store.recent) > store.capacity {
		evicted := store.recent[0]
		store.recent = append([]acceptedIndex(nil), store.recent[1:]...)
		delete(store.seenIDs, evicted.id)
		if acceptedAt, exists := store.dedupeKeys[evicted.dedupeKey]; exists && acceptedAt.Equal(evicted.acceptedAt) {
			delete(store.dedupeKeys, evicted.dedupeKey)
		}
	}
	return EventAccepted, envelope
}

func (store *Store) Revision() uint64 {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.revision
}

func (store *Store) NextRevision() uint64 {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.revision++
	return store.revision
}

func (store *Store) Events(limit int) []protocol.Envelope {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if limit <= 0 || limit > len(store.events) {
		limit = len(store.events)
	}
	return append([]protocol.Envelope(nil), store.events[len(store.events)-limit:]...)
}

func (store *Store) RecordACK(ack protocol.ACK, received time.Time) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.acks = append(store.acks, ACKRecord{ACK: ack, Received: received})
	if len(store.acks) > store.capacity {
		store.acks = append([]ACKRecord(nil), store.acks[len(store.acks)-store.capacity:]...)
	}
}

func (store *Store) ACKs() []ACKRecord {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return append([]ACKRecord(nil), store.acks...)
}
