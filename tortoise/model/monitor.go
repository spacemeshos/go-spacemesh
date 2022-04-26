package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Monitor is an interface for monitors.
type Monitor interface {
	OnEvent(Event)
	Test()
}

// Event is received by monitors, and used for validation.
type Event interface{}

// EventVerified is raised when tortoise outputs verified layer.
type EventVerified struct {
	ID       string
	Layer    types.LayerID
	Verified types.LayerID
	Revert   bool
}

func newVerifiedMonitor(tb testing.TB, genesis types.LayerID) *verifiedMonitor {
	return &verifiedMonitor{
		tb:       tb,
		genesis:  genesis,
		verified: map[string]types.LayerID{},
	}
}

type verifiedMonitor struct {
	tb       testing.TB
	last     types.LayerID
	genesis  types.LayerID
	verified map[string]types.LayerID
}

func (m *verifiedMonitor) OnEvent(event Event) {
	switch ev := event.(type) {
	case EventVerified:
		m.verified[ev.ID] = ev.Verified
		if ev.Layer.After(m.last) {
			m.last = ev.Layer
		}
	}
}

func (m *verifiedMonitor) Test() {
	if !m.last.After(m.genesis) {
		return
	}
	for id, verified := range m.verified {
		require.Equal(m.tb, m.last.Sub(1), verified, "id=%s", id)
	}
}
