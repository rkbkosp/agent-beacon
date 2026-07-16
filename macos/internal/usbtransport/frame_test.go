package usbtransport

import (
	"bytes"
	"testing"
)

func TestFrameRoundTripAcrossChunksAndMultipleMessages(t *testing.T) {
	payloads := [][]byte{{'{', '"', 'x', '"', ':', 0, '1', '}'}, []byte(`{"v":2}`)}
	var wire []byte
	for _, payload := range payloads {
		encoded, err := Encode(payload)
		if err != nil {
			t.Fatal(err)
		}
		wire = append(wire, encoded...)
	}
	var decoder Decoder
	var got [][]byte
	for _, chunk := range [][]byte{wire[:3], wire[3:11], wire[11:]} {
		frames, rejected := decoder.Feed(chunk)
		if rejected != 0 {
			t.Fatalf("rejected %d valid frames", rejected)
		}
		got = append(got, frames...)
	}
	if len(got) != len(payloads) {
		t.Fatalf("decoded %d frames, want %d", len(got), len(payloads))
	}
	for index := range payloads {
		if !bytes.Equal(got[index], payloads[index]) {
			t.Fatalf("frame %d = %q, want %q", index, got[index], payloads[index])
		}
	}
}

func TestFrameRejectsCorruptionAndResynchronizes(t *testing.T) {
	bad, err := Encode([]byte("broken"))
	if err != nil {
		t.Fatal(err)
	}
	bad[len(bad)/2] ^= 0x40
	good, err := Encode([]byte("good"))
	if err != nil {
		t.Fatal(err)
	}
	var decoder Decoder
	frames, rejected := decoder.Feed(append(bad, good...))
	if rejected != 1 || len(frames) != 1 || string(frames[0]) != "good" {
		t.Fatalf("frames=%q rejected=%d", frames, rejected)
	}
}

func TestFrameLimitsAndOversizedStream(t *testing.T) {
	if _, err := Encode(nil); err == nil {
		t.Fatal("empty payload was accepted")
	}
	if _, err := Encode(make([]byte, MaxPayloadBytes+1)); err == nil {
		t.Fatal("oversized payload was accepted")
	}
	maximum := make([]byte, MaxPayloadBytes)
	for index := range maximum {
		maximum[index] = byte(index)
	}
	wire, err := Encode(maximum)
	if err != nil || len(wire) > maxWireBytes {
		t.Fatalf("maximum frame len=%d err=%v", len(wire), err)
	}
	var decoder Decoder
	noise := bytes.Repeat([]byte{1}, maxEncodedBytes+1)
	frames, rejected := decoder.Feed(append(noise, 0))
	if rejected != 1 || len(frames) != 0 {
		t.Fatalf("oversized stream frames=%d rejected=%d", len(frames), rejected)
	}
}
