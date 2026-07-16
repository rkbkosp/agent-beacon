package usbtransport

import "testing"

func FuzzDecoderFeed(f *testing.F) {
	valid, err := Encode([]byte(`{"v":2,"type":"heartbeat"}`))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte{0})
	f.Add([]byte{1, 2, 3, 0, 4, 5, 0})
	f.Add(bytesRepeat(0xff, 4096))
	f.Add(append(bytesRepeat(0x01, maxEncodedBytes+10), 0))

	f.Fuzz(func(t *testing.T, data []byte) {
		var decoder Decoder
		frames, rejected := decoder.Feed(data)
		if rejected < 0 {
			t.Fatalf("negative rejected count %d", rejected)
		}
		for _, frame := range frames {
			if len(frame) == 0 || len(frame) > MaxPayloadBytes {
				t.Fatalf("decoded payload size out of range: %d", len(frame))
			}
		}
		// Second feed of zeros should not panic and should flush state.
		_, _ = decoder.Feed([]byte{0, 0, 0})
	})
}

func FuzzEncodeDecodeRoundTrip(f *testing.F) {
	f.Add([]byte(`hello`))
	f.Add([]byte(`{"v":2}`))
	f.Add(bytesRepeat('x', 1024))
	f.Add(bytesRepeat(0, 64))
	f.Add(bytesRepeat(0xff, 512))

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) == 0 || len(payload) > MaxPayloadBytes {
			return
		}
		wire, err := Encode(payload)
		if err != nil {
			t.Fatalf("encode valid payload: %v", err)
		}
		var decoder Decoder
		frames, rejected := decoder.Feed(wire)
		if rejected != 0 {
			t.Fatalf("round-trip rejected encoded frame: %d", rejected)
		}
		if len(frames) != 1 {
			t.Fatalf("expected 1 frame, got %d", len(frames))
		}
		if string(frames[0]) != string(payload) {
			t.Fatalf("round-trip mismatch: got %q want %q", frames[0], payload)
		}
	})
}

func bytesRepeat(value byte, count int) []byte {
	out := make([]byte, count)
	for i := range out {
		out[i] = value
	}
	return out
}
