package hare4

import "github.com/spacemeshos/go-spacemesh/common/types"

type Tracer interface {
	OnStart(types.LayerID)
	OnStop(types.LayerID)
	OnActive([]*types.HareEligibility)
	OnMessageSent(*Message)
	OnMessageReceived(*Message)
}

var _ Tracer = noopTracer{}

type noopTracer struct{}

func (noopTracer) OnStart(types.LayerID) {}

func (noopTracer) OnStop(types.LayerID) {}

func (noopTracer) OnActive([]*types.HareEligibility) {}

func (noopTracer) OnMessageSent(*Message) {}

func (noopTracer) OnMessageReceived(*Message) {}
