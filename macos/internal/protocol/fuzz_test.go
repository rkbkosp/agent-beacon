package protocol

import (
	"encoding/json"
	"testing"
	"time"
	"unicode/utf8"
)

func withDecodeBudget(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("decode exceeded 250ms budget (possible DoS hang)")
	}
}

func FuzzDecode(f *testing.F) {
	seedValid, err := NewEnvelope("seed-1", TypeHeartbeat, 1, time.Now().UTC(), Heartbeat{})
	if err != nil {
		f.Fatal(err)
	}
	valid, err := json.Marshal(seedValid)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"v":2}`))
	f.Add([]byte("\x00\xff\xfe not utf8 \xff"))
	f.Add(bytesRepeat('{', MaxMessageBytes+1))
	f.Add([]byte(`{"v":2,"id":"x","type":"notification","ts":"2020-01-01T00:00:00Z","revision":0,"payload":{}}`))
	f.Add([]byte(`{"v":1,"id":"x","type":"hello","ts":"2020-01-01T00:00:00Z","revision":0,"payload":{"role":"device","device_id":"d1","protocol_version":2}}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		withDecodeBudget(t, func() {
			envelope, err := Decode(data)
			if err != nil {
				return
			}
			if envelope.V != Version {
				t.Fatalf("accepted unsupported version %d", envelope.V)
			}
			if envelope.ID == "" || len(envelope.ID) > 64 {
				t.Fatalf("accepted invalid id %q", envelope.ID)
			}
			if !utf8.Valid(data) {
				t.Fatal("accepted non-UTF-8 input")
			}
			if len(data) > MaxMessageBytes {
				t.Fatal("accepted oversized message")
			}
			if err := envelope.Validate(); err != nil {
				t.Fatalf("Decode succeeded but Validate failed: %v", err)
			}
		})
	})
}

func FuzzDecodeDeviceMessage(f *testing.F) {
	hello, err := NewEnvelope("hello-1", TypeHello, 0, time.Now().UTC(), Hello{
		Role: "device", DeviceID: "dev-1", ProtocolVersion: Version, AuthToken: "tok",
	})
	if err != nil {
		f.Fatal(err)
	}
	helloBytes, err := json.Marshal(hello)
	if err != nil {
		f.Fatal(err)
	}
	ack := ACK{
		V: Version, Type: TypeACK, ID: "ack-1", DeviceID: "dev-1",
		Status: ACKReceived, At: time.Now().UTC(),
	}
	ackBytes, err := json.Marshal(ack)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(helloBytes)
	f.Add(ackBytes)
	f.Add([]byte(`{"type":"ack"}`))
	f.Add([]byte(`{"v":2,"type":"ack","id":"a","device_id":"d","status":"shown","at":"2020-01-01T00:00:00Z"}`))
	f.Add(bytesRepeat('A', MaxMessageBytes+8))

	f.Fuzz(func(t *testing.T, data []byte) {
		withDecodeBudget(t, func() {
			message, err := DecodeDeviceMessage(data)
			if err != nil {
				return
			}
			if message.ACK == nil && message.Envelope == nil {
				t.Fatal("decoded empty device message")
			}
			if message.ACK != nil {
				if err := message.ACK.Validate(); err != nil {
					t.Fatalf("ACK accepted without validation: %v", err)
				}
			}
			if message.Envelope != nil {
				if err := message.Envelope.Validate(); err != nil {
					t.Fatalf("envelope accepted without validation: %v", err)
				}
			}
			if len(data) > MaxMessageBytes || !utf8.Valid(data) {
				t.Fatal("accepted invalid device message size/encoding")
			}
		})
	})
}

func bytesRepeat(value byte, count int) []byte {
	out := make([]byte, count)
	for i := range out {
		out[i] = value
	}
	return out
}
