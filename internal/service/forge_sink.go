package service

import (
	"context"

	"clawbench/internal/forge"
)

// ForgeCompositeSink fans a derived event out to several sinks. Notification
// and task triggering are deliberately separate concerns: each can be disabled
// or fail without affecting the other.
type ForgeCompositeSink struct {
	sinks []ForgeChangeSink
}

// NewForgeCompositeSink builds a fan-out sink. Nil sinks are ignored.
func NewForgeCompositeSink(sinks ...ForgeChangeSink) *ForgeCompositeSink {
	kept := make([]ForgeChangeSink, 0, len(sinks))
	for _, s := range sinks {
		if s != nil {
			kept = append(kept, s)
		}
	}
	return &ForgeCompositeSink{sinks: kept}
}

// HandleChange forwards the event to every sink.
func (c *ForgeCompositeSink) HandleChange(ctx context.Context, repo ForgeRepoRef, item forge.Item, change forge.Change) {
	for _, s := range c.sinks {
		s.HandleChange(ctx, repo, item, change)
	}
}
