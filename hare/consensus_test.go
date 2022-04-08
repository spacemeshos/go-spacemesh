package hare

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
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
	brokers     []*Broker
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

func (his *HareSuite) fill(set *Set, begin, end int) {
	for i := begin; i <= end; i++ {
		his.initialSets[i] = set
	}
}

func (his *HareSuite) waitForTermination() {
	for _, p := range his.procs {
		<-p.CloseChannel()
		his.outputs = append(his.outputs, p.s)
	}
	for _, b := range his.brokers {
		b.Close()
		<-b.CloseChannel()
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
	for _, v := range his.outputs[0].elements() {
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

func createConsensusProcess(tb testing.TB, isHonest bool, cfg config.Config, oracle fullRolacle, network pubsub.PublishSubsciber, initialSet *Set, layer types.LayerID, name string) (*consensusProcess, *Broker) {
	broker := buildBroker(tb, name)
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.Start(context.TODO())
	network.Register(ProtoName, broker.HandleMessage)
	output := make(chan TerminationOutput)
	certs := make(chan CertificationOutput)
	signing := signing2.NewEdSigner()
	oracle.Register(isHonest, signing.PublicKey().String())
	proc := newConsensusProcess(cfg, layer, initialSet, oracle, broker.mockStateQ, 10, signing,
		types.NodeID{Key: signing.PublicKey().String(), VRFPublicKey: []byte{}}, network, output, certs, truer{},
		newRoundClockFromCfg(logtest.New(tb), cfg), logtest.New(tb).WithName(signing.PublicKey().ShortString()))
	c, _ := broker.Register(context.TODO(), proc.ID())
	proc.SetInbox(c)

	go func() {
		// consume certifications output channel
		for range certs {
		}
	}()
	go func() {
		// consume reports output channel
		for range output {
		}
	}()
	return proc, broker.Broker
}

func TestConsensusFixedOracle(t *testing.T) {
	if skipBlackBox {
		t.Skip()
	}
	test := newConsensusTest()
	cfg := config.Config{N: 16, F: 8, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000}

	totalNodes := 20
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(ctx, totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(value1)
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	creationFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, true, cfg, oracle, ps, test.initialSets[i], types.NewLayerID(1), t.Name())
		test.procs = append(test.procs, proc)
		test.brokers = append(test.brokers, broker)
		i++
	}
	test.Create(totalNodes, creationFunc)
	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func TestSingleValueForHonestSet(t *testing.T) {
	if skipBlackBox {
		t.Skip()
	}

	test := newConsensusTest()

	// Larger values may trigger race detector failures because of 8128 goroutines limit.
	cfg := config.Config{N: 10, F: 5, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000}
	totalNodes := 10

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(ctx, totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(value1)
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	creationFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, true, cfg, oracle, ps, test.initialSets[i], types.NewLayerID(1), t.Name())
		test.procs = append(test.procs, proc)
		test.brokers = append(test.brokers, broker)
		i++
	}
	test.Create(totalNodes, creationFunc)
	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func TestAllDifferentSet(t *testing.T) {
	if skipBlackBox {
		t.Skip()
	}

	test := newConsensusTest()

	cfg := config.Config{N: 10, F: 5, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000}
	totalNodes := 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(ctx, totalNodes)
	require.NoError(t, err)

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
	oracle := eligibility.New(logtest.New(t))
	i := 0
	creationFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, true, cfg, oracle, ps, test.initialSets[i], types.NewLayerID(1), t.Name())
		test.procs = append(test.procs, proc)
		test.brokers = append(test.brokers, broker)
		i++
	}
	test.Create(cfg.N, creationFunc)
	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func TestSndDelayedDishonest(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	test := newConsensusTest()

	cfg := config.Config{N: 16, F: 8, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000}
	totalNodes := 20

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(ctx, totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	honest1 := NewSetFromValues(value1, value2, value4, value5)
	honest2 := NewSetFromValues(value1, value3, value4, value6)
	dishonest := NewSetFromValues(value3, value5, value6, value7)
	test.fill(honest1, 0, 15)
	test.fill(honest2, 16, totalNodes/2)
	test.fill(dishonest, totalNodes/2+1, totalNodes-1)
	test.honestSets = []*Set{honest1, honest2}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	honestFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, true, cfg, oracle, ps, test.initialSets[i], types.NewLayerID(1), t.Name())
		test.brokers = append(test.brokers, broker)
		test.procs = append(test.procs, proc)
		i++
	}

	// create honest
	test.Create(totalNodes/2+1, honestFunc)

	// create dishonest
	dishonestFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, false, cfg, oracle,
			&delayeadPubSub{ps: ps, sendDelay: 5 * time.Second},
			test.initialSets[i], types.NewLayerID(1), t.Name())
		test.dishonest = append(test.dishonest, proc)
		test.brokers = append(test.brokers, broker)
		i++
	}
	test.Create(totalNodes/2-1, dishonestFunc)
	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 40*time.Second)
}

func TestRecvDelayedDishonest(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	test := newConsensusTest()

	cfg := config.Config{N: 16, F: 8, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000}
	totalNodes := 20

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(ctx, totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	honest1 := NewSetFromValues(value1, value2, value4, value5)
	honest2 := NewSetFromValues(value1, value3, value4, value6)
	dishonest := NewSetFromValues(value3, value5, value6, value7)
	test.fill(honest1, 0, 15)
	test.fill(honest2, 16, totalNodes/2)
	test.fill(dishonest, totalNodes/2+1, totalNodes-1)
	test.honestSets = []*Set{honest1, honest2}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	honestFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, true, cfg, oracle, ps, test.initialSets[i], types.NewLayerID(1), t.Name())
		test.procs = append(test.procs, proc)
		test.brokers = append(test.brokers, broker)
		i++
	}

	// create honest
	test.Create(totalNodes/2+1, honestFunc)

	// create dishonest
	dishonestFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, false, cfg, oracle,
			&delayeadPubSub{ps: ps, recvDelay: 5 * time.Second},
			test.initialSets[i], types.NewLayerID(1), t.Name())
		test.dishonest = append(test.dishonest, proc)
		test.brokers = append(test.brokers, broker)
		i++
	}
	test.Create(totalNodes/2-1, dishonestFunc)
	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 40*time.Second)
}

type delayeadPubSub struct {
	ps                   pubsub.PublishSubsciber
	recvDelay, sendDelay time.Duration
}

func (ps *delayeadPubSub) Publish(ctx context.Context, protocol string, msg []byte) error {
	if ps.sendDelay != 0 {
		rng := time.Duration(rand.Uint32()) * time.Second % ps.sendDelay
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(rng):
		}
	}

	if err := ps.ps.Publish(ctx, protocol, msg); err != nil {
		return fmt.Errorf("publish message: %w", err)
	}

	return nil
}

func (ps *delayeadPubSub) Register(protocol string, handler pubsub.GossipHandler) {
	if ps.recvDelay != 0 {
		handler = func(ctx context.Context, pid p2p.Peer, msg []byte) pubsub.ValidationResult {
			rng := time.Duration(rand.Uint32()) * time.Second % ps.recvDelay
			select {
			case <-ctx.Done():
				return pubsub.ValidationIgnore
			case <-time.After(rng):
			}
			return handler(ctx, pid, msg)
		}
	}
	ps.ps.Register(protocol, handler)
}
