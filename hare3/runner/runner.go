package runner

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/hare3"
)

type ProtocolRunner struct {
	messages  chan MultiMsg
	handler   hare3.Handler
	protocol  hare3.Protocol
	roundTime time.Duration
	gossiper  NetworkGossiper
}

func (r *ProtocolRunner) Run() []hare3.Hash20 {
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
		case multiMsg := <-r.messages:
			// It is assumed that messages received here have had their
			// signature verified by the broker and that the broker made use of
			// the message sid to route the message to this instance.
			m := multiMsg.Message
			if r.handler.HandleMsg(m.key, m.values, m.round) {
				// Send raw message to peers
				err := r.gossiper.Gossip(multiMsg.RawMessage)
				if err != nil {
					logerr(err)
				}
			}
		}
	}
}

type MultiMsg struct {
	RawMessage []byte
	Message    Msg
}

type Msg struct {
	sid, key []byte
	values   []hare3.Hash20
	round    int8
}

type NetworkGossiper interface {
	Gossip(msg []byte) error
}

func buildEncodedOutputMessgae(m *hare3.OutputMessage) ([]byte, error) {
	return nil, nil
}

func logerr(err error) {
}
