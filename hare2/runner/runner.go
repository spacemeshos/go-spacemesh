package runner

import (
	"time"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/hare2"
)

type ProtocolRunner struct {
	messages  chan []byte
	handler   hare2.Handler
	protocol  hare2.Protocol
	roundTime time.Duration
	gossiper  NetworkGossiper
}

func (r *ProtocolRunner) Run() []hare2.Hash20 {
	t := time.NewTicker(r.roundTime)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			toSend, output := r.protocol.NextRound()
			if toSend != nil {
				msg, err := buildEncodedOutputMessgae(toSend)
				if err != nil {
					// This should never happen
					panic(err)
				}
				r.gossiper.Gossip(msg)
			}
			if output != nil {
				return output
			}
		case msg := <-r.messages:
			id, values, round, grade, err := getHandlerInputs(msg)
			if err != nil {
				// We drop the message in case of error
				logerr(err)
				continue
			}
			if r.handler.HandleMsg(id, values, round, grade) {
				// Send message to peers
				err := r.gossiper.Gossip(msg)
				if err != nil {
					logerr(err)
				}
			}
		}
	}
}

func getHandlerInputs(msg []byte) (id hare2.Hash20, values []hare2.Hash20, round hare2.AbsRound, grade uint8, err error) {
	w := &Wrapper{}

	err = codec.Decode(msg, w)
	if err != nil {
		return hare2.Hash20{}, nil, 0, 0, err
	}
	err = verify(w.sig, w.msg)
	if err != nil {
		return hare2.Hash20{}, nil, 0, 0, err
	}
	m := &Msg{}
	err = codec.Decode(msg, m)
	if err != nil {
		return hare2.Hash20{}, nil, 0, 0, err
	}

	round = hare2.AbsRound(m.round)
	switch round.Type() {
	case hare2.Propose:
		grade, err = gradeKey3(m.key)
		if err != nil {
			return hare2.Hash20{}, nil, 0, 0, err
		}
	default:
		grade, err = gradeKey5(m.key)
		if err != nil {
			return hare2.Hash20{}, nil, 0, 0, err
		}
	}
	id = hashBytes(m.key)
	return id, values, round, grade, nil
}

type Wrapper struct {
	msg, sig []byte
}

func (m *Wrapper) DecodeScale(d *scale.Decoder) (int, error) {
	return 0, nil
}

type Msg struct {
	sid, key []byte
	values   []hare2.Hash20
	round    int8
}

func (m *Msg) DecodeScale(d *scale.Decoder) (int, error) {
	return 0, nil
}

type NetworkGossiper interface {
	Gossip(msg []byte) error
}

func buildEncodedOutputMessgae(m *hare2.OutputMessage) ([]byte, error) {
	return nil, nil
}

func hashBytes(v []byte) hare2.Hash20 {
	return hare2.Hash20{}
}

func verify(sig, data []byte) error {
	return nil
}

func gradeKey3(key []byte) (uint8, error) {
	return 0, nil
}

func gradeKey5(key []byte) (uint8, error) {
	return 0, nil
}

func logerr(err error) {
}
