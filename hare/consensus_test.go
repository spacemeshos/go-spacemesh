package hare

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// Test the consensus process as a whole

type fullRolacle interface {
	registrable
	Rolacle
}

type HareSuite struct {
	termination chan struct{}
	procs       []*consensusProcess
	dishonest   []*consensusProcess
	brokers     []*Broker
	initialSets []*Set // all initial sets
	honestSets  []*Set // initial sets of honest
	outputs     []*Set
}

func newHareSuite() *HareSuite {
	hs := new(HareSuite)
	hs.termination = make(chan struct{})
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
		<-p.ctx.Done()
		his.outputs = append(his.outputs, p.value)
	}
	for _, b := range his.brokers {
		b.Close()
	}

	close(his.termination)
}

func (his *HareSuite) WaitForTimedTermination(t *testing.T, timeout time.Duration) {
	timer := time.After(timeout)
	go his.waitForTermination()
	select {
	case <-timer:
		t.Fatal("Timeout")
		return
	case <-his.termination:
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
	for _, v := range his.outputs[0].ToSlice() {
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

func startProcs(wg *sync.WaitGroup, procs []*consensusProcess) {
	for _, proc := range procs {
		proc.Start()
		wg.Done()
	}
}

func (test *ConsensusTest) Start() {
	var wg sync.WaitGroup
	wg.Add(len(test.procs))
	wg.Add(len(test.dishonest))
	go startProcs(&wg, test.procs)
	go startProcs(&wg, test.dishonest)
	wg.Wait()
}

func createConsensusProcess(tb testing.TB, ctx context.Context, sig *signing.EdSigner, isHonest bool, cfg config.Config, oracle fullRolacle, network pubsub.PublishSubsciber, initialSet *Set, layer types.LayerID) (*consensusProcess, *Broker) {
	broker := buildBroker(tb, sig.PublicKey().ShortString())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.Start(ctx)
	network.Register(pubsub.HareProtocol, broker.HandleMessage)
	output := make(chan TerminationOutput, 1)
	oracle.Register(isHonest, sig.NodeID())
	proc := newConsensusProcess(ctx, cfg, layer, initialSet, oracle, broker.mockStateQ, sig,
		sig.NodeID(), network, output, truer{},
		newRoundClockFromCfg(logtest.New(tb), cfg),
		make(chan types.MalfeasanceGossip, cfg.N),
		logtest.New(tb).WithName(sig.PublicKey().ShortString()))
	c, err := broker.Register(ctx, proc.ID())
	require.NoError(tb, err)
	proc.SetInbox(c)

	return proc, broker.Broker
}

func TestConsensusFixedOracle(t *testing.T) {
	test := newConsensusTest()
	cfg := config.Config{N: 16, F: 8, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}

	totalNodes := 20
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
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
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
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
	test := newConsensusTest()

	// Larger values may trigger race detector failures because of 8128 goroutines limit.
	cfg := config.Config{N: 10, F: 5, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}
	totalNodes := 10

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
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
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
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
	test := newConsensusTest()

	cfg := config.Config{N: 10, F: 5, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}
	totalNodes := 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
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
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
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

	cfg := config.Config{N: 16, F: 8, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}
	totalNodes := 20

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
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
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
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
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, ctx, sig, false, cfg, oracle,
			&delayedPubSub{ps: ps, sendDelay: 5 * time.Second},
			test.initialSets[i], instanceID1)
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

	cfg := config.Config{N: 16, F: 8, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}
	totalNodes := 20

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
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
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
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
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, ctx, sig, false, cfg, oracle,
			&delayedPubSub{ps: ps, recvDelay: 5 * time.Second},
			test.initialSets[i], instanceID1)
		test.dishonest = append(test.dishonest, proc)
		test.brokers = append(test.brokers, broker)
		i++
	}
	test.Create(totalNodes/2-1, dishonestFunc)
	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 40*time.Second)
}

type delayedPubSub struct {
	ps                   pubsub.PublishSubsciber
	recvDelay, sendDelay time.Duration
}

func (ps *delayedPubSub) Publish(ctx context.Context, protocol string, msg []byte) error {
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

func (ps *delayedPubSub) Register(protocol string, handler pubsub.GossipHandler) {
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

func TestEquivocation(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	test := newConsensusTest()

	cfg := config.Config{N: 16, F: 8, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}
	totalNodes := 20

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(value1, value2)
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	badGuy, err := signing.NewEdSigner()
	require.NoError(t, err)
	dishonestFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, ctx, badGuy, false, cfg, oracle,
			&equivocatePubSub{ps: ps, sig: badGuy},
			test.initialSets[i], instanceID1)
		test.dishonest = append(test.dishonest, proc)
		test.brokers = append(test.brokers, broker)
		i++
	}
	// create dishonest
	test.Create(1, dishonestFunc)

	honestFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		proc, broker := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
		test.procs = append(test.procs, proc)
		test.brokers = append(test.brokers, broker)
		i++
	}
	// create honest
	test.Create(totalNodes-1, honestFunc)

	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 40*time.Second)
	// every node should detect a pre-round equivocator
	for _, c := range append(test.procs, test.dishonest...) {
		require.Len(t, c.malCh, 1)
		gossip := <-c.malCh
		require.True(t, bytes.Equal(gossip.Eligibility.PubKey, badGuy.PublicKey().Bytes()))
	}
}

type equivocatePubSub struct {
	ps  pubsub.PublishSubsciber
	sig *signing.EdSigner
}

func (eps *equivocatePubSub) Publish(ctx context.Context, protocol string, data []byte) error {
	msg, err := MessageFromBuffer(data)
	if err != nil {
		return fmt.Errorf("decode published data: %w", err)
	}
	if msg.InnerMsg.Type == pre {
		msg.InnerMsg.Values = msg.InnerMsg.Values[1:]
		msg.Signature = eps.sig.Sign(msg.SignedBytes())
		encoded, err := codec.Encode(&msg)
		if err != nil {
			log.With().Fatal("failed to encode equivocation data", log.Err(err))
		}
		if err = eps.ps.Publish(ctx, protocol, encoded); err != nil {
			return fmt.Errorf("publish equivocate message: %w", err)
		}
	}
	if err = eps.ps.Publish(ctx, protocol, data); err != nil {
		return fmt.Errorf("publish original message: %w", err)
	}
	return nil
}

func (eps *equivocatePubSub) Register(protocol string, handler pubsub.GossipHandler) {
	eps.ps.Register(protocol, handler)
}
