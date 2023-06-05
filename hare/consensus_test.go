package hare

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// Test the consensus process as a whole

type registrable interface {
	Register(isHonest bool, id types.NodeID)
	Unregister(isHonest bool, id types.NodeID)
}

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

type testCP struct {
	cp     *consensusProcess
	broker *Broker
	mch    chan *types.MalfeasanceGossip
}

func createConsensusProcess(
	tb testing.TB,
	ctx context.Context,
	sig *signing.EdSigner,
	isHonest bool,
	cfg config.Config,
	oracle fullRolacle,
	network pubsub.PublishSubsciber,
	initialSet *Set,
	layer types.LayerID,
) *testCP {
	broker := buildBroker(tb, sig.PublicKey().ShortString())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).AnyTimes()
	broker.Start(ctx)
	network.Register(pubsub.HareProtocol, broker.HandleMessage)
	output := make(chan report, 1)
	wc := make(chan wcReport, 1)
	oracle.Register(isHonest, sig.NodeID())
	edVerifier, err := signing.NewEdVerifier()
	require.NoError(tb, err)
	c, err := broker.Register(ctx, layer)
	require.NoError(tb, err)
	nonce := types.VRFPostIndex(1)
	mch := make(chan *types.MalfeasanceGossip, cfg.N)
	comm := communication{
		inbox:  c,
		mchOut: mch,
		report: output,
		wc:     wc,
	}
	proc := newConsensusProcess(
		ctx,
		cfg,
		layer,
		initialSet,
		oracle,
		broker.mockStateQ,
		sig,
		edVerifier,
		sig.NodeID(),
		&nonce,
		network,
		comm,
		truer{},
		newRoundClockFromCfg(logtest.New(tb), cfg),
		logtest.New(tb).WithName(sig.PublicKey().ShortString()),
	)
	return &testCP{cp: proc, broker: broker.Broker, mch: mch}
}

// Test - runs a single CP for more than one iteration.
func TestConsensus_MultipleIterations(t *testing.T) {
	test := newConsensusTest()

	totalNodes := 15
	cfg := config.Config{N: totalNodes, WakeupDelta: time.Second, RoundDuration: time.Second, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 100, Hdist: 20}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(types.ProposalID{1})
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	creationFunc := func() {
		host := mesh.Hosts()[i]
		ps, err := pubsub.New(ctx, logtest.New(t), host, pubsub.DefaultConfig())
		require.NoError(t, err)
		p2pm := &p2pManipulator{nd: ps, stalledLayer: instanceID1, err: errors.New("fake err")}
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		tcp := createConsensusProcess(t, ctx, sig, true, cfg, oracle, p2pm, test.initialSets[i], instanceID1)
		test.procs = append(test.procs, tcp.cp)
		test.brokers = append(test.brokers, tcp.broker)
		i++
	}
	test.Create(totalNodes, creationFunc)
	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 40*time.Second)
}

func TestConsensusFixedOracle(t *testing.T) {
	test := newConsensusTest()
	cfg := config.Config{N: 16, RoundDuration: 2 * time.Second, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}

	totalNodes := 20
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(types.ProposalID{1})
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	creationFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		tcp := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
		test.procs = append(test.procs, tcp.cp)
		test.brokers = append(test.brokers, tcp.broker)
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
	cfg := config.Config{N: 10, RoundDuration: 2 * time.Second, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}
	totalNodes := 10

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(types.ProposalID{1})
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	creationFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		tcp := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
		test.procs = append(test.procs, tcp.cp)
		test.brokers = append(test.brokers, tcp.broker)
		i++
	}
	test.Create(totalNodes, creationFunc)
	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 30*time.Second)
}

func TestAllDifferentSet(t *testing.T) {
	test := newConsensusTest()

	cfg := config.Config{N: 10, RoundDuration: 2 * time.Second, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}
	totalNodes := 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)

	base := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	test.initialSets[0] = base
	test.initialSets[1] = NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3})
	test.initialSets[2] = NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{4})
	test.initialSets[3] = NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{5})
	test.initialSets[4] = NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{6})
	test.initialSets[5] = NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{7})
	test.initialSets[6] = NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{8})
	test.initialSets[7] = NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{9})
	test.initialSets[8] = NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{10})
	test.initialSets[9] = NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{3}, types.ProposalID{4})
	test.honestSets = []*Set{base}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	creationFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		tcp := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
		test.procs = append(test.procs, tcp.cp)
		test.brokers = append(test.brokers, tcp.broker)
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

	cfg := config.Config{N: 16, RoundDuration: 2 * time.Second, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}
	totalNodes := 20

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	honest1 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{4}, types.ProposalID{5})
	honest2 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{3}, types.ProposalID{4}, types.ProposalID{6})
	dishonest := NewSetFromValues(types.ProposalID{3}, types.ProposalID{5}, types.ProposalID{6}, types.ProposalID{7})
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
		tcp := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
		test.brokers = append(test.brokers, tcp.broker)
		test.procs = append(test.procs, tcp.cp)
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
		tcp := createConsensusProcess(t, ctx, sig, false, cfg, oracle,
			&delayedPubSub{ps: ps, sendDelay: 5 * time.Second},
			test.initialSets[i], instanceID1)
		test.dishonest = append(test.dishonest, tcp.cp)
		test.brokers = append(test.brokers, tcp.broker)
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

	cfg := config.Config{N: 16, RoundDuration: 2 * time.Second, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}
	totalNodes := 20

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	honest1 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2}, types.ProposalID{4}, types.ProposalID{5})
	honest2 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{3}, types.ProposalID{4}, types.ProposalID{6})
	dishonest := NewSetFromValues(types.ProposalID{3}, types.ProposalID{5}, types.ProposalID{6}, types.ProposalID{7})
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
		tcp := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
		test.procs = append(test.procs, tcp.cp)
		test.brokers = append(test.brokers, tcp.broker)
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
		tcp := createConsensusProcess(t, ctx, sig, false, cfg, oracle,
			&delayedPubSub{ps: ps, recvDelay: 5 * time.Second},
			test.initialSets[i], instanceID1)
		test.dishonest = append(test.dishonest, tcp.cp)
		test.brokers = append(test.brokers, tcp.broker)
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
		handler = func(ctx context.Context, pid p2p.Peer, msg []byte) error {
			rng := time.Duration(rand.Uint32()) * time.Second % ps.recvDelay
			select {
			case <-ctx.Done():
				return errors.New("ignore")
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

	cfg := config.Config{N: 16, RoundDuration: 2 * time.Second, ExpectedLeaders: 5, LimitIterations: 1000, Hdist: 20}
	totalNodes := 20

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mesh, err := mocknet.FullMeshLinked(totalNodes)
	require.NoError(t, err)

	test.initialSets = make([]*Set, totalNodes)
	set1 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	test.fill(set1, 0, totalNodes-1)
	test.honestSets = []*Set{set1}
	oracle := eligibility.New(logtest.New(t))
	i := 0
	badGuy, err := signing.NewEdSigner()
	require.NoError(t, err)
	mchs := make([]chan *types.MalfeasanceGossip, 0, totalNodes)
	dishonestFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		tcp := createConsensusProcess(t, ctx, badGuy, false, cfg, oracle,
			&equivocatePubSub{ps: ps, sig: badGuy},
			test.initialSets[i], instanceID1)
		test.dishonest = append(test.dishonest, tcp.cp)
		test.brokers = append(test.brokers, tcp.broker)
		mchs = append(mchs, tcp.mch)
		i++
	}
	// create dishonest
	test.Create(1, dishonestFunc)

	honestFunc := func() {
		ps, err := pubsub.New(ctx, logtest.New(t), mesh.Hosts()[i], pubsub.DefaultConfig())
		require.NoError(t, err)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		tcp := createConsensusProcess(t, ctx, sig, true, cfg, oracle, ps, test.initialSets[i], instanceID1)
		test.procs = append(test.procs, tcp.cp)
		test.brokers = append(test.brokers, tcp.broker)
		mchs = append(mchs, tcp.mch)
		i++
	}
	// create honest
	test.Create(totalNodes-1, honestFunc)

	require.NoError(t, mesh.ConnectAllButSelf())
	test.Start()
	test.WaitForTimedTermination(t, 40*time.Second)
	// every node should detect a pre-round equivocator
	for _, ch := range mchs {
		require.Len(t, ch, 1)
		gossip := <-ch
		require.Equal(t, badGuy.NodeID(), gossip.Eligibility.NodeID)
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
	if msg.Type == pre {
		msg.Values = []types.ProposalID{types.RandomProposalID()}
		msg.Signature = eps.sig.Sign(signing.HARE, msg.SignedBytes())
		msg.SmesherID = eps.sig.NodeID()
		if err = eps.ps.Publish(ctx, protocol, msg.Bytes()); err != nil {
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
