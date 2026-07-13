package state

import (
	"testing"
	"time"

	"agent-beacon/internal/protocol"
)

func event(t *testing.T, id, key string, now time.Time) protocol.Envelope {
	t.Helper()
	n := protocol.Notification{
		Category: protocol.CategoryAgent, Kind: "agent.done", Source: "mock", SubjectID: "pane-1",
		Theme: protocol.ThemeGreen, Urgency: protocol.UrgencyNormal, Priority: 50, DedupeKey: key,
		Title: "Agent completed", Detail: "Tests passed", DisplayMS: 4000, ExpiresAt: now.Add(time.Minute),
	}
	envelope, err := protocol.NewEnvelope(id, protocol.TypeNotification, 1, now, n)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func TestStoreDeduplicatesIDAndKeyWithinWindow(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	store := NewStore(time.Minute, 100)

	if result := store.AddEvent(event(t, "evt-1", "agent:1:done", now), now); result != EventAccepted {
		t.Fatalf("first result = %s", result)
	}
	if result := store.AddEvent(event(t, "evt-1", "agent:other", now), now); result != EventDuplicate {
		t.Fatalf("duplicate id result = %s", result)
	}
	if result := store.AddEvent(event(t, "evt-2", "agent:1:done", now.Add(30*time.Second)), now.Add(30*time.Second)); result != EventDuplicate {
		t.Fatalf("duplicate key result = %s", result)
	}
	if result := store.AddEvent(event(t, "evt-3", "agent:1:done", now.Add(61*time.Second)), now.Add(61*time.Second)); result != EventAccepted {
		t.Fatalf("expired dedupe window result = %s", result)
	}
}

func TestStoreRecordsFlatDeviceACK(t *testing.T) {
	store := NewStore(time.Minute, 10)
	ack := protocol.ACK{V: 2, Type: protocol.TypeACK, ID: "evt-1", DeviceID: "device-1", Status: protocol.ACKShown, At: time.Now()}
	store.RecordACK(ack, time.Now())
	acks := store.ACKs()
	if len(acks) != 1 || acks[0].ACK.DeviceID != "device-1" || acks[0].ACK.Status != protocol.ACKShown {
		t.Fatalf("unexpected ACKs: %+v", acks)
	}
}

func TestStoreAssignsContinuousRevisionsWithoutDuplicateGaps(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	store := NewStore(time.Minute, 10)

	result, first := store.AcceptEvent(event(t, "evt-1", "key-1", now), now)
	if result != EventAccepted || first.Revision != 1 {
		t.Fatalf("first result=%s revision=%d", result, first.Revision)
	}
	result, _ = store.AcceptEvent(event(t, "evt-duplicate", "key-1", now), now)
	if result != EventDuplicate {
		t.Fatalf("duplicate result=%s", result)
	}
	result, second := store.AcceptEvent(event(t, "evt-2", "key-2", now), now)
	if result != EventAccepted || second.Revision != 2 || store.Revision() != 2 {
		t.Fatalf("second result=%s revision=%d store=%d", result, second.Revision, store.Revision())
	}
}

func TestStoreBoundsHistoryAndHonorsLimit(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	store := NewStore(time.Minute, 2)
	for index, values := range [][2]string{{"evt-1", "key-1"}, {"evt-2", "key-2"}, {"evt-3", "key-3"}} {
		at := now.Add(time.Duration(index) * time.Second)
		if result := store.AddEvent(event(t, values[0], values[1], at), at); result != EventAccepted {
			t.Fatalf("event %d result = %s", index, result)
		}
	}
	if got := store.Events(1); len(got) != 1 || got[0].ID != "evt-3" {
		t.Fatalf("limited events = %+v", got)
	}
}
