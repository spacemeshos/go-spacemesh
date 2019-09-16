package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"testing"
	"time"
)

type p2pManipulator struct {
	nd     *service.Node
	bCount int
	err    error
}

func (m *p2pManipulator) RegisterGossipProtocol(protocol string) chan service.GossipMessage {
	ch := m.nd.RegisterGossipProtocol(protocol)
	wch := make(chan service.GossipMessage)

	go func() {
		for {
			x := <-ch
			wch <- x
		}
	}()

	return wch
}

func (m *p2pManipulator) Broadcast(protocol string, payload []byte) error {
	defer func() {
		m.bCount++
	}()

	if m.err != nil && m.bCount > 1 && m.bCount < 3 {
		log.Error("Not broadcasting in manipulator %v", m.bCount)
		return m.err
	}

	e := m.nd.Broadcast(protocol, payload)
	return e
}

type trueOracle struct {
}

func (trueOracle) Register(isHonest bool, id string) {
}

func (trueOracle) Unregister(isHonest bool, id string) {
}

func (trueOracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	return true, nil
}

func (trueOracle) Proof(layer types.LayerID, round int32) ([]byte, error) {
	x := make([]byte, 100)
	return x, nil
}

func (trueOracle) IsIdentityActiveOnConsensusView(edId string, layer types.LayerID) (bool, error) {
	return true, nil
}

// run CP for more than one iteration
func Test_consensusIterations(t *testing.T) {
	test := newConsensusTest()

	totalNodes := 20
	cfg := config.Config{N: totalNodes, F: totalNodes/2 - 1, RoundDuration: 1, ExpectedLeaders: 5}
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(value1)
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := &trueOracle{}
	i := 0
	creationFunc := func() {
		s := sim.NewNode()
		p2pm := &p2pManipulator{nd: s, err: errors.New("fake err")}
		proc := createConsensusProcess(true, cfg, oracle, p2pm, test.initialSets[i], t.Name())
		test.procs = append(test.procs, proc)
		i++
	}
	test.Create(totalNodes, creationFunc)
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}
