package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	//_ "net/http/pprof"
	"testing"
	"time"
)

//func init() {
//	go http.ListenAndServe(":3030", nil)
//}

// Test the consensus process as a whole

type HareSuite struct {
	termination Closer
	procs       []*ConsensusProcess
	dishonest   []*ConsensusProcess
	initialSets []*Set // all initial sets
	honestSets  []*Set // initial sets of honest
	outputs     []*Set
	name        string
}

func newHareSuite() *HareSuite {
	hs := new(HareSuite)
	hs.termination = NewCloser()
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
	// build world of values (U)
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
	for _, v := range his.outputs[0].values {
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

func startProcs(procs []*ConsensusProcess) {
	for _, proc := range procs {
		proc.Start()
	}
}

func (test *ConsensusTest) Start() {
	go startProcs(test.procs)
	go startProcs(test.dishonest)
}

func TestSingleValueForHonestSet(t *testing.T) {
	test := newConsensusTest()

	cfg := config.Config{N: 50, F: 25, SetSize: 1, RoundDuration: time.Second * time.Duration(1)}
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, cfg.N)
	set1 := NewSetFromValues(value1)
	test.fill(set1, 0, cfg.N-1)
	test.honestSets = []*Set{set1}
	oracle := NewMockHashOracle(cfg.N)
	i := 0
	creationFunc := func() {
		s := sim.NewNode()
		broker := NewBroker(s)
		output := make(chan TerminationOutput, 1)
		signing := NewMockSigning()
		oracle.Register(signing.Verifier())
		proc := NewConsensusProcess(cfg, *instanceId1, test.initialSets[i], oracle, signing, s, output)
		broker.Register(proc)
		broker.Start()
		test.procs = append(test.procs, proc)
		i++
	}
	test.Create(cfg.N, creationFunc)
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func TestAllDifferentSet(t *testing.T) {
	test := newConsensusTest()

	cfg := config.Config{N: 10, F: 5, SetSize: 5, RoundDuration: time.Second * time.Duration(1)}
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, cfg.N)

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
	oracle := NewMockHashOracle(cfg.N)
	i := 0
	creationFunc := func() {
		s := sim.NewNode()
		broker := NewBroker(s)
		output := make(chan TerminationOutput, 1)
		signing := NewMockSigning()
		oracle.Register(signing.Verifier())
		proc := NewConsensusProcess(cfg, *instanceId1, test.initialSets[i], oracle, signing, s, output)
		broker.Register(proc)
		broker.Start()
		test.procs = append(test.procs, proc)
		i++
	}
	test.Create(cfg.N, creationFunc)
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func TestDelayedDishonest(t *testing.T) {
	test := newConsensusTest()

	cfg := config.Config{N: 50, F: 25, SetSize: 5, RoundDuration: time.Second * time.Duration(2)}
	sim := service.NewSimulator()
	test.initialSets = make([]*Set, cfg.N)
	set1 := NewSetFromValues(value1)
	test.fill(set1, 0, cfg.N-1)
	test.honestSets = []*Set{set1}
	oracle := NewMockHashOracle(cfg.N)
	i := 0
	honestFunc := func() {
		s := sim.NewNode()
		broker := NewBroker(s)
		output := make(chan TerminationOutput, 1)
		signing := NewMockSigning()
		oracle.Register(signing.Verifier())
		proc := NewConsensusProcess(cfg, *instanceId1, test.initialSets[i], oracle, signing, s, output)
		broker.Register(proc)
		broker.Start()
		test.procs = append(test.procs, proc)
		i++
	}

	// create honest
	test.Create(cfg.N/2+1, honestFunc)

	// create dishonest
	dishonestFunc := func() {
		s := sim.NewFaulty(time.Second * time.Duration(5)) // 5 sec delay
		broker := NewBroker(s)
		output := make(chan TerminationOutput, 1)
		signing := NewMockSigning()
		oracle.Register(signing.Verifier())
		proc := NewConsensusProcess(cfg, *instanceId1, test.initialSets[i], oracle, signing, s, output)
		broker.Register(proc)
		broker.Start()
		test.dishonest = append(test.dishonest, proc)
		i++
	}
	test.Create(cfg.N/2-1, dishonestFunc)

	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}
