package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type envelope struct {
	Format   int             `json:"format"`
	Payload  json.RawMessage `json:"payload"`
	Checksum string          `json:"sha256"`
}

func encodeCheckpoint(value State) ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal state: %w", err)
	}
	sum := sha256.Sum256(payload)
	return json.Marshal(envelope{Format: 1, Payload: payload, Checksum: hex.EncodeToString(sum[:])})
}

func decodeCheckpoint(data []byte) (State, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return State{}, fmt.Errorf("%w: decode envelope: %v", ErrCorrupt, err)
	}
	if env.Format != 1 {
		return State{}, fmt.Errorf("%w: unsupported checkpoint format %d", ErrCorrupt, env.Format)
	}
	sum := sha256.Sum256(env.Payload)
	if hex.EncodeToString(sum[:]) != env.Checksum {
		return State{}, fmt.Errorf("%w: checkpoint checksum mismatch", ErrCorrupt)
	}
	var value State
	if err := json.Unmarshal(env.Payload, &value); err != nil {
		return State{}, fmt.Errorf("%w: decode state: %v", ErrCorrupt, err)
	}
	if err := value.Validate(); err != nil {
		return State{}, err
	}
	return value, nil
}
