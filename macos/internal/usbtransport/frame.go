package usbtransport

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

const (
	MaxPayloadBytes  = 64 * 1024
	frameHeaderBytes = 8
	frameCRCBytes    = 4
	maxRawBytes      = frameHeaderBytes + MaxPayloadBytes + frameCRCBytes
	maxEncodedBytes  = maxRawBytes + maxRawBytes/254 + 1
	maxWireBytes     = maxEncodedBytes + 1
	frameVersion     = 1
)

var frameMagic = [2]byte{'A', 'B'}

func Encode(payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(payload) > MaxPayloadBytes {
		return nil, errors.New("USB payload size is invalid")
	}
	raw := make([]byte, frameHeaderBytes+len(payload)+frameCRCBytes)
	raw[0], raw[1], raw[2] = frameMagic[0], frameMagic[1], frameVersion
	binary.BigEndian.PutUint32(raw[4:8], uint32(len(payload)))
	copy(raw[frameHeaderBytes:], payload)
	checksumOffset := frameHeaderBytes + len(payload)
	binary.BigEndian.PutUint32(raw[checksumOffset:], crc32.ChecksumIEEE(raw[:checksumOffset]))
	encoded := cobsEncode(raw)
	if len(encoded) > maxEncodedBytes {
		return nil, errors.New("encoded USB frame is too large")
	}
	return append(encoded, 0), nil
}

func decodeFrame(encoded []byte) ([]byte, error) {
	raw, err := cobsDecode(encoded)
	if err != nil || len(raw) < frameHeaderBytes+frameCRCBytes {
		return nil, errors.New("invalid COBS frame")
	}
	if raw[0] != frameMagic[0] || raw[1] != frameMagic[1] || raw[2] != frameVersion || raw[3] != 0 {
		return nil, errors.New("invalid USB frame header")
	}
	payloadLength := int(binary.BigEndian.Uint32(raw[4:8]))
	if payloadLength < 1 || payloadLength > MaxPayloadBytes ||
		len(raw) != frameHeaderBytes+payloadLength+frameCRCBytes {
		return nil, errors.New("invalid USB frame length")
	}
	checksumOffset := frameHeaderBytes + payloadLength
	if binary.BigEndian.Uint32(raw[checksumOffset:]) != crc32.ChecksumIEEE(raw[:checksumOffset]) {
		return nil, errors.New("invalid USB frame checksum")
	}
	return append([]byte(nil), raw[frameHeaderBytes:checksumOffset]...), nil
}

type Decoder struct {
	encoded  []byte
	dropping bool
}

func (decoder *Decoder) Feed(data []byte) (frames [][]byte, rejected int) {
	for _, value := range data {
		if value != 0 {
			if decoder.dropping {
				continue
			}
			if len(decoder.encoded) >= maxEncodedBytes {
				decoder.encoded = decoder.encoded[:0]
				decoder.dropping = true
				rejected++
				continue
			}
			decoder.encoded = append(decoder.encoded, value)
			continue
		}
		if decoder.dropping {
			decoder.dropping = false
			continue
		}
		if len(decoder.encoded) == 0 {
			continue
		}
		payload, err := decodeFrame(decoder.encoded)
		decoder.encoded = decoder.encoded[:0]
		if err != nil {
			rejected++
			continue
		}
		frames = append(frames, payload)
	}
	return frames, rejected
}

func cobsEncode(input []byte) []byte {
	output := make([]byte, 1, len(input)+len(input)/254+1)
	codeIndex := 0
	code := byte(1)
	for _, value := range input {
		if value == 0 {
			output[codeIndex] = code
			codeIndex = len(output)
			output = append(output, 0)
			code = 1
			continue
		}
		output = append(output, value)
		code++
		if code == 0xff {
			output[codeIndex] = code
			codeIndex = len(output)
			output = append(output, 0)
			code = 1
		}
	}
	output[codeIndex] = code
	return output
}

func cobsDecode(input []byte) ([]byte, error) {
	output := make([]byte, 0, len(input))
	for readIndex := 0; readIndex < len(input); {
		code := int(input[readIndex])
		readIndex++
		if code == 0 || code-1 > len(input)-readIndex {
			return nil, errors.New("invalid COBS code")
		}
		output = append(output, input[readIndex:readIndex+code-1]...)
		readIndex += code - 1
		if code != 0xff && readIndex < len(input) {
			output = append(output, 0)
		}
	}
	return output, nil
}
