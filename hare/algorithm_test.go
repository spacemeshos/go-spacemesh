package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"testing"
)

func getPublicKey(t *testing.T) crypto.PublicKey {
	_, pub, err := crypto.GenerateKeyPair()

	if err != nil {
		assert.Fail(t, "failed generating key")
	}

	return pub
}

// test that a message to a specific layer is delivered by the broker
func TestConsensusProcess_StartTwice(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	broker := NewBroker(n1)
	s := Set{}
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(getPublicKey(t), *Layer1, s, oracle, signing, n1)
	broker.Register(Layer1, proc)
	err := proc.Start()
	assert.Equal(t, nil, err)
	err = proc.Start()
	assert.Equal(t, "instance already started", err.Error())
}

func TestConsensusProcess_eventLoop(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	broker := NewBroker(n1)
	s := Set{}
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(getPublicKey(t), *Layer1, s, oracle, signing, n1)
	broker.Register(Layer1, proc)
	go proc.eventLoop()
	n2.Broadcast(ProtoName, []byte{})

	proc.Close()
	<-proc.CloseChannel()
}

func TestConsensusProcess_handleMessage(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	broker := NewBroker(n1)
	s := Set{}
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(getPublicKey(t), *Layer1, s, oracle, signing, n1)
	broker.Register(Layer1, proc)

	x, err := NewMessageBuilder().SetIteration(0).SetLayer(*Layer1).Sign(proc.signing)

	if err != nil {
		log.Error("Could not sign msg")
	}

	m := x.Build()

	proc.handleMessage(m)
	assert.Equal(t, 1, len(proc.knowledge))
	proc.nextRound()
	assert.Equal(t, 0, len(proc.knowledge))
}

func TestConsensusProcess_nextRound(t *testing.T) {
	sim := service.NewSimulator()
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
