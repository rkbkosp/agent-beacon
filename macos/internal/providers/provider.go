package providers

import (
	"context"

	"agent-beacon/internal/protocol"
)

type Update struct {
	Patch        protocol.StatePatch
	Notification *protocol.Notification
}

type Health struct {
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail,omitempty"`
}

type Provider interface {
	Name() string
	Start(ctx context.Context, out chan<- Update) error
	Snapshot(ctx context.Context) (protocol.StatePatch, error)
	Health(ctx context.Context) Health
}
