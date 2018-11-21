package hare

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/simulator"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func getPublicKey(t *testing.T) crypto.PublicKey {
	_, pub, err := crypto.GenerateKeyPair()

	if err != nil {
		assert.Fail(t, "failed generating key")
	}

	return pub
}

// test that a message to a specific layer is delivered by the broker
func TestConsensusProcess_Start(t *testing.T) {
	sim := simulator.New()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	broker := NewBroker(n1)
	s := Set{}
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(getPublicKey(t), *Layer1, s, oracle, signing, n1)
	broker.Register(Layer1, proc)
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

	to := time.Second * time.Duration(1) // run 1 sec
	timer := time.NewTicker(to)

	select {
	case <-timer.C:
		assert.True(t, true)
	}
}

func TestConsensusProcess_handleMessage(t *testing.T) {
	sim := simulator.New()
	n1 := sim.NewNode()

	broker := NewBroker(n1)
	s := Set{}
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(getPublicKey(t), *Layer1, s, oracle, signing, n1)
	broker.Register(Layer1, proc)

	x, err := proc.buildStatusMessage()

	if err != nil {
		assert.Fail(t, "error building status message")
	}

	hareMsg := &pb.HareMessage{}
	err = proto.Unmarshal(x, hareMsg)

	if err != nil {
		assert.Fail(t, "protobuf error")
	}

	proc.handleMessage(hareMsg)
	assert.Equal(t, 1, len(proc.knowledge))
	proc.nextRound()
	assert.Equal(t, 0, len(proc.knowledge))
}

func TestConsensusProcess_nextRound(t *testing.T) {
	sim := simulator.New()
	n1 := sim.NewNode()

	broker := NewBroker(n1)
	s := Set{}
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(getPublicKey(t), *Layer1, s, oracle, signing, n1)
	broker.Register(Layer1, proc)

	proc.nextRound()
	assert.Equal(t, uint32(1), proc.k)
	proc.nextRound()
	assert.Equal(t, uint32(2), proc.k)
}
