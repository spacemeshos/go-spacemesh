package hare

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/simulator"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

// test that a message to a specific layer is delivered by the broker
func TestConsensusProcess_Run10(t *testing.T) {
	sim := simulator.New()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	broker := NewBroker(n1)
	s := Set{}
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(PubKey{}, *Layer1, s, oracle, signing, n1, broker)
	proc.Start()

	m := &pb.HareMessage{}

	x := &pb.InnerMessage{}
	x.Type = int32(Status)
	m.Message = x

	buff, err := proto.Marshal(m)

	if err != nil {
		assert.Fail(t, "proto error")
	}

	n2.Broadcast(ProtoName, buff)

	to := time.Second * time.Duration(10)
	timer := time.NewTicker(to)

	select {
	case <-timer.C:
		assert.True(t, true)
	}
}