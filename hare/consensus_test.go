package hare

import (
	"context"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	signing2 "github.com/spacemeshos/go-spacemesh/signing"
)

// Test the consensus process as a whole

var skipBlackBox = false

type fullRolacle interface {
	registrable
	Rolacle
}

type HareSuite struct {
	termination util.Closer
	procs       []*consensusProcess
	dishonest   []*consensusProcess
	initialSets []*Set // all initial sets
	honestSets  []*Set // initial sets of honest
	outputs     []*Set
	name        string
}

func newHareSuite() *HareSuite {
	hs := new(HareSuite)
	hs.termination = util.NewCloser()
	hs.outputs = make([]*Set, 0)

	return hs
}

func (his *HareSuite) fill(set *Set, begin int, end int) {
	for i := begin; i <= end; i++ {
		his.initialSets[i] = set
	}
}

func (his *HareSuite) waitForTermination() {
	for _, p := range his.procs {
		<-p.CloseChannel()
		his.outputs = append(his.outputs, p.s)
	}

	his.termination.Close()
}

func (his *HareSuite) WaitForTimedTermination(t *testing.T, timeout time.Duration) {
	timer := time.After(timeout)
	go his.waitForTermination()
	select {
	case <-timer:
		t.Fatal("Timeout")
		return
	case <-his.termination.CloseChannel():
		his.checkResult(t)
		return
	}
}

func (his *HareSuite) checkResult(t *testing.T) {
	// build world of Values (U)
	u := his.initialSets[0]
	for i := 1; i < len(his.initialSets); i++ {
		u = u.Union(his.initialSets[i])
	}

	// check consistency
	for i := 0; i < len(his.outputs)-1; i++ {
		if !his.outputs[i].Equals(his.outputs[i+1]) {
			t.Errorf("Consistency check failed: Expected: %v Actual: %v", his.outputs[i], his.outputs[i+1])
		}
	}

	// build intersection
	inter := u.Intersection(his.honestSets[0])
	for i := 1; i < len(his.honestSets); i++ {
		inter = inter.Intersection(his.honestSets[i])
	}

	// check that the output contains the intersection
	if !inter.IsSubSetOf(his.outputs[0]) {
		t.Error("Validity 1 failed: output does not contain the intersection of honest parties")
	}

	// build union
	union := his.honestSets[0]
	for i := 1; i < len(his.honestSets); i++ {
		union = union.Union(his.honestSets[i])
	}

	// check that the output has no intersection with the complement of the union of honest
	for v := range his.outputs[0].values {
		if union.Complement(u).Contains(v) {
			t.Error("Validity 2 failed: unexpected value encountered: ", v)
		}
	}
}

type ConsensusTest struct {
	*HareSuite
}

func newConsensusTest() *ConsensusTest {
	ct := new(ConsensusTest)
	ct.HareSuite = newHareSuite()

	return ct
}

func (test *ConsensusTest) Create(N int, create func()) {
	for i := 0; i < N; i++ {
		create()
	}
}

func startProcs(procs []*consensusProcess) {
	for _, proc := range procs {
		proc.Start(context.TODO())
	}
}

func (test *ConsensusTest) Start() {
	go startProcs(test.procs)
	go startProcs(test.dishonest)
}

func createConsensusProcess(isHonest bool, cfg config.Config, oracle fullRolacle, network NetworkService, initialSet *Set, layer instanceID, name string) *consensusProcess {
	broker := buildBroker(network, name)
	broker.Start(context.TODO())
	output := make(chan TerminationOutput, 1)
	signing := signing2.NewEdSigner()
	oracle.Register(isHonest, signing.PublicKey().String())
	proc := newConsensusProcess(cfg, layer, initialSet, oracle, NewMockStateQuerier(), 10, signing, types.NodeID{Key: signing.PublicKey().String(), VRFPublicKey: []byte{}}, network, output, truer{}, log.NewDefault(signing.PublicKey().ShortString()))
	c, _ := broker.Register(context.TODO(), proc.ID())
	proc.SetInbox(c)

	return proc
}

func TestConsensusFixedOracle(t *testing.T) {
	if skipBlackBox {
		t.Skip()
	}

	test := newConsensusTest()

	cfg := config.Config{N: 16, F: 8, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000}
	sim := service.NewSimulator()
	totalNodes := 20
	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(value1)
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := eligibility.New()
	i := 0
	creationFunc := func() {
		s := sim.NewNode()
		proc := createConsensusProcess(true, cfg, oracle, s, test.initialSets[i], 1, t.Name())
		test.procs = append(test.procs, proc)
		i++
	}
	test.Create(totalNodes, creationFunc)
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func TestSingleValueForHonestSet(t *testing.T) {
	if skipBlackBox {
		t.Skip()
	}

	test := newConsensusTest()

	cfg := config.Config{N: 50, F: 25, RoundDuration: 3, ExpectedLeaders: 5, LimitIterations: 1000}
	totalNodes := 50
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(value1)
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := eligibility.New()
	i := 0
	creationFunc := func() {
		s := sim.NewNode()
		proc := createConsensusProcess(true, cfg, oracle, s, test.initialSets[i], 1, t.Name())
		test.procs = append(test.procs, proc)
		i++
	}
	test.Create(totalNodes, creationFunc)
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func TestAllDifferentSet(t *testing.T) {
	if skipBlackBox {
		t.Skip()
	}

	test := newConsensusTest()

	cfg := config.Config{N: 10, F: 5, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000}
	sim := service.NewSimulator()
	totalNodes := 10
	test.initialSets = make([]*Set, totalNodes)

	base := NewSetFromValues(value1, value2)
	test.initialSets[0] = base
	test.initialSets[1] = NewSetFromValues(value1, value2, value3)
	test.initialSets[2] = NewSetFromValues(value1, value2, value4)
	test.initialSets[3] = NewSetFromValues(value1, value2, value5)
	test.initialSets[4] = NewSetFromValues(value1, value2, value6)
	test.initialSets[5] = NewSetFromValues(value1, value2, value7)
	test.initialSets[6] = NewSetFromValues(value1, value2, value8)
	test.initialSets[7] = NewSetFromValues(value1, value2, value9)
	test.initialSets[8] = NewSetFromValues(value1, value2, value10)
	test.initialSets[9] = NewSetFromValues(value1, value2, value3, value4)
	test.honestSets = []*Set{base}
	oracle := eligibility.New()
	i := 0
	creationFunc := func() {
		s := sim.NewNode()
		proc := createConsensusProcess(true, cfg, oracle, s, test.initialSets[i], 1, t.Name())
		test.procs = append(test.procs, proc)
		i++
	}
	test.Create(cfg.N, creationFunc)
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func TestSndDelayedDishonest(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	test := newConsensusTest()

	cfg := config.Config{N: 50, F: 25, RoundDuration: 3, ExpectedLeaders: 5, LimitIterations: 1000}
	totalNodes := 50
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, totalNodes)
	honest1 := NewSetFromValues(value1, value2, value4, value5)
	honest2 := NewSetFromValues(value1, value3, value4, value6)
	dishonest := NewSetFromValues(value3, value5, value6, value7)
	test.fill(honest1, 0, 15)
	test.fill(honest2, 16, totalNodes/2)
	test.fill(dishonest, totalNodes/2+1, totalNodes-1)
	test.honestSets = []*Set{honest1, honest2}
	oracle := eligibility.New()
	i := 0
	honestFunc := func() {
		s := sim.NewNode()
		proc := createConsensusProcess(true, cfg, oracle, s, test.initialSets[i], 1, t.Name())
		test.procs = append(test.procs, proc)
		i++
	}

	// create honest
	test.Create(totalNodes/2+1, honestFunc)

	// create dishonest
	dishonestFunc := func() {
		s := sim.NewFaulty(true, 5, 0) // only broadcast delay
		proc := createConsensusProcess(false, cfg, oracle, s, test.initialSets[i], 1, t.Name())
		test.dishonest = append(test.dishonest, proc)
		i++
	}
	test.Create(totalNodes/2-1, dishonestFunc)

	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func TestRecvDelayedDishonest(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	test := newConsensusTest()

	cfg := config.Config{N: 50, F: 25, RoundDuration: 3, ExpectedLeaders: 5, LimitIterations: 1000}
	totalNodes := 50
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, totalNodes)
	honest1 := NewSetFromValues(value1, value2, value4, value5)
	honest2 := NewSetFromValues(value1, value3, value4, value6)
	dishonest := NewSetFromValues(value3, value5, value6, value7)
	test.fill(honest1, 0, 15)
	test.fill(honest2, 16, totalNodes/2)
	test.fill(dishonest, totalNodes/2+1, totalNodes-1)
	test.honestSets = []*Set{honest1, honest2}
	oracle := eligibility.New()
	i := 0
	honestFunc := func() {
		s := sim.NewNode()
		proc := createConsensusProcess(true, cfg, oracle, s, test.initialSets[i], 1, t.Name())
		test.procs = append(test.procs, proc)
		i++
	}

	// create honest
	test.Create(totalNodes/2+1, honestFunc)

	// create dishonest
	dishonestFunc := func() {
		s := sim.NewFaulty(true, 0, 10) // delay rcv
		proc := createConsensusProcess(false, cfg, oracle, s, test.initialSets[i], 1, t.Name())
		test.dishonest = append(test.dishonest, proc)
		i++
	}
	test.Create(totalNodes/2-1, dishonestFunc)

	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}
