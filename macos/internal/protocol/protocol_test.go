package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNotificationEnvelopeV2RoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 14, 14, 30, 1, 0, time.FixedZone("CST", 8*60*60))
	notification := Notification{
		Category: CategoryAgent, Kind: "agent.done", Source: "herdr", SubjectID: "w1:p1",
		Theme: ThemeGreen, Urgency: UrgencyNormal, Priority: 50,
		DedupeKey: "agent:w1:p1:session-abc:done:42", SupersedeKey: "agent:w1:p1",
		GroupKey: "agent:done", Title: "Agent completed", Detail: "Chrome Plugin - review",
		SourceLabel: "Herdr", DisplayMS: 4000, ExpiresAt: now.Add(time.Minute),
		ReplayAfterInterrupt: false, MaxReplays: 0,
	}
	envelope, err := NewEnvelope("evt-1", TypeNotification, 301, now, notification)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodePayload[Notification](decoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.V != 2 || got.Kind != "agent.done" || got.Urgency != UrgencyNormal || got.Priority != 50 {
		t.Fatalf("unexpected notification: envelope=%+v notification=%+v", decoded, got)
	}
}

func TestDecodeRejectsV1ForbiddenCategoryAndInvalidUTF8(t *testing.T) {
	inputs := [][]byte{
		[]byte(`{"v":1,"id":"evt-1","type":"notification","ts":"2026-07-14T12:00:00+08:00","revision":1,"payload":{}}`),
		[]byte(`{"v":2,"id":"evt-1","type":"notification","ts":"2026-07-14T12:00:00+08:00","revision":1,"payload":{"category":"message","kind":"message.new","source":"mock","subject_id":"x","theme":"blue","urgency":"normal","priority":10,"dedupe_key":"x","title":"x","display_ms":4000,"expires_at":"2026-07-14T12:10:00+08:00"}}`),
		append([]byte(`{"v":2,"id":"evt-`), 0xff),
	}
	for _, input := range inputs {
		if _, err := Decode(input); err == nil {
			t.Fatalf("expected Decode to reject %q", input)
		}
	}
}

func TestACKV2Validation(t *testing.T) {
	ack := ACK{V: Version, Type: TypeACK, ID: "evt-1", DeviceID: "device-1", Status: ACKShown, At: time.Now()}
	data, err := json.Marshal(ack)
	if err != nil {
		t.Fatal(err)
	}
	message, err := DecodeDeviceMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if message.ACK == nil || message.ACK.Status != ACKShown {
		t.Fatalf("decoded ACK = %+v", message)
	}

	ack.Status = "displayed"
	if err := ack.Validate(); err == nil {
		t.Fatal("legacy ACK status must be rejected")
	}
}

func TestSnapshotV2RejectsOldTopLevelFields(t *testing.T) {
	now := time.Now()
	payload := Snapshot{
		Clock: ClockState{Timezone: "Asia/Shanghai", ServerTime: now},
		Codex: CodexState{Homes: []CodexHome{{ID: "main", Label: "MAIN", WeeklyRemainingPercent: 18,
			ResetCardsAvailable: intPointer(2), Freshness: FreshnessFresh}}},
		Agents:  AgentsState{Provider: "herdr", Connected: true, UpdatedAt: now},
		Weather: WeatherState{Location: "Hangzhou", Provider: "qweather", UpdatedAt: now},
		System:  SystemState{BridgeOnline: true, OverallFreshness: FreshnessFresh},
	}
	if _, err := NewEnvelope("snapshot-1", TypeSnapshot, 1, now, payload); err != nil {
		t.Fatal(err)
	}

	legacy := []byte(`{"v":2,"id":"snapshot-2","type":"snapshot","ts":"2026-07-14T12:00:00+08:00","revision":1,"payload":{"clock":{"timezone":"Asia/Shanghai","server_time":"2026-07-14T12:00:00+08:00"},"codex":{"homes":[],"relay":{}},"agents":{"provider":"herdr","connected":true,"updated_at":"2026-07-14T12:00:00+08:00","items":[]},"weather":{"location":"Hangzhou","provider":"qweather","updated_at":"2026-07-14T12:00:00+08:00"},"system":{"bridge_online":true,"overall_freshness":"fresh"},"tasks":{}}}`)
	if _, err := Decode(legacy); err == nil {
		t.Fatal("snapshot with tasks must be rejected")
	}
}

func intPointer(value int) *int { return &value }
