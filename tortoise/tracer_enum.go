package tortoise

import (
	"fmt"

	"github.com/spacemeshos/go-scale"
)

func NewEventEnum() EventEnum {
	enum := EventEnum{types: map[uint16]traceEvent{}}
	enum.Register(&ConfigTrace{})
	enum.Register(&WeakCoinTrace{})
	enum.Register(&BeaconTrace{})
	enum.Register(&AtxTrace{})
	enum.Register(&BallotTrace{})
	enum.Register(&DecodeBallotTrace{})
	enum.Register(&StoreBallotTrace{})
	enum.Register(&EncodeVotesTrace{})
	enum.Register(&TallyTrace{})
	enum.Register(&BlockTrace{})
	enum.Register(&HareTrace{})
	enum.Register(&ResultsTrace{})
	enum.Register(&UpdatesTrace{})
	return enum
}

type EventEnum struct {
	types map[eventType]traceEvent
}

func (e *EventEnum) Register(ev traceEvent) {
	e.types[ev.Type()] = ev
}

func (e *EventEnum) Decode(dec *scale.Decoder) (traceEvent, error) {
	typ, _, err := scale.DecodeCompact16(dec)
	if err != nil {
		return nil, err
	}
	ev := e.types[typ]
	if ev == nil {
		return nil, fmt.Errorf("type %d is not registered", typ)
	}
	obj := ev.New()
	_, err = obj.DecodeScale(dec)
	if err != nil {
		return nil, err
	}
	return obj, nil
}
