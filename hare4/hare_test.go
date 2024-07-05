package hare4

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare4/eligibility"
	hmock "github.com/spacemeshos/go-spacemesh/hare4/mocks"
	"github.com/spacemeshos/go-spacemesh/layerpatrol"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const layersPerEpoch = 4

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)
	res := m.Run()
	os.Exit(res)
}

type tester struct {
	testing.TB

	rng           *rand.Rand
	start         time.Time
	cfg           Config
	layerDuration time.Duration
	beacon        types.Beacon
	genesis       types.LayerID
}

type waiter struct {
	lid types.LayerID
	ch  chan struct{}
}

// timesync.Nodeclock time can't be mocked nicely because of ticks.
type testNodeClock struct {
	mu      sync.Mutex
	started types.LayerID
	waiters []waiter

	genesis       time.Time
	layerDuration time.Duration
}

func (t *testNodeClock) CurrentLayer() types.LayerID {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.started
}

func (t *testNodeClock) LayerToTime(lid types.LayerID) time.Time {
	return t.genesis.Add(time.Duration(lid) * t.layerDuration)
}

func (t *testNodeClock) StartLayer(lid types.LayerID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.started = lid
	for _, w := range t.waiters {
		if w.lid <= lid {
			select {
			case <-w.ch:
			default:
				close(w.ch)
			}
		}
	}
}

func (t *testNodeClock) AwaitLayer(lid types.LayerID) <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	ch := make(chan struct{})
	if lid <= t.started {
		close(ch)
		return ch
	}
	t.waiters = append(t.waiters, waiter{lid: lid, ch: ch})
	return ch
}

type node struct {
	t *tester

	i          int
	clock      clockwork.FakeClock
	nclock     *testNodeClock
	signer     *signing.EdSigner
	registered []*signing.EdSigner
	vrfsigner  *signing.VRFSigner
	atx        *types.ActivationTx
	oracle     *eligibility.Oracle
	db         *sql.Database
	atxsdata   *atxsdata.Data
	proposals  *store.Store

	ctrl                *gomock.Controller
	mpublisher          *pmocks.MockPublishSubsciber
	msyncer             *smocks.MockSyncStateProvider
	mverifier           *hmock.Mockverifier
	mockStreamRequester *hmock.MockstreamRequester
	patrol              *layerpatrol.LayerPatrol
	tracer              *testTracer
	hare                *Hare
}

func (n *node) withClock() *node {
	n.clock = clockwork.NewFakeClockAt(n.t.start)
	return n
}

func (n *node) withSigner() *node {
	signer, err := signing.NewEdSigner(signing.WithKeyFromRand(n.t.rng))
	require.NoError(n.t, err)
	n.signer = signer
	n.vrfsigner = signer.VRFSigner()
	return n
}

func (n *node) reuseSigner(signer *signing.EdSigner) *node {
	n.signer = signer
	n.vrfsigner = signer.VRFSigner()
	return n
}

func (n *node) withDb() *node {
	n.db = sql.InMemory()
	n.atxsdata = atxsdata.New()
	n.proposals = store.New()
	return n
}

func (n *node) withAtx(min, max int) *node {
	atx := &types.ActivationTx{
		PublishEpoch: n.t.genesis.GetEpoch(),
		TickCount:    1,
		SmesherID:    n.signer.NodeID(),
	}
	if max-min > 0 {
		atx.NumUnits = uint32(n.t.rng.Intn(max-min) + min)
	} else {
		atx.NumUnits = uint32(min)
	}
	id := types.ATXID{}
	n.t.rng.Read(id[:])
	atx.SetID(id)
	atx.SetReceived(n.t.start)
	atx.VRFNonce = types.VRFPostIndex(n.t.rng.Uint64())

	n.atx = atx
	return n
}

func (n *node) withController() *node {
	n.ctrl = gomock.NewController(n.t)
	return n
}

func (n *node) withSyncer() *node {
	n.msyncer = smocks.NewMockSyncStateProvider(n.ctrl)
	n.msyncer.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	return n
}

func (n *node) withVerifier() *node {
	n.mverifier = hmock.NewMockverifier(n.ctrl)
	return n
}

func (n *node) withOracle() *node {
	beaconget := smocks.NewMockBeaconGetter(n.ctrl)
	beaconget.EXPECT().GetBeacon(gomock.Any()).DoAndReturn(func(epoch types.EpochID) (types.Beacon, error) {
		return beacons.Get(n.db, epoch)
	}).AnyTimes()
	n.oracle = eligibility.New(
		beaconget,
		n.db,
		n.atxsdata,
		signing.NewVRFVerifier(),
		layersPerEpoch,
	)
	return n
}

func (n *node) withPublisher() *node {
	n.mpublisher = pmocks.NewMockPublishSubsciber(n.ctrl)
	n.mpublisher.EXPECT().Register(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return n
}

func (n *node) withStreamRequester() *node {
	n.mockStreamRequester = hmock.NewMockstreamRequester(n.ctrl)
	return n
}

func (n *node) withHare() *node {
	logger := logtest.New(n.t).Named(fmt.Sprintf("hare=%d", n.i))
	n.nclock = &testNodeClock{
		genesis:       n.t.start,
		layerDuration: n.t.layerDuration,
	}
	tracer := newTestTracer(n.t)
	n.tracer = tracer
	n.patrol = layerpatrol.New()
	var verify verifier
	if n.mverifier != nil {
		verify = n.mverifier
	} else {
		verify = signing.NewEdVerifier()
	}
	n.hare = New(
		n.nclock,
		n.mpublisher,
		n.db,
		n.atxsdata,
		n.proposals,
		verify,
		n.oracle,
		n.msyncer,
		n.patrol,
		WithConfig(n.t.cfg),
		WithLogger(logger.Zap()),
		WithWallclock(n.clock),
		WithTracer(tracer),
	)
	n.register(n.signer)
	return n
}

func (n *node) waitEligibility() {
	n.tracer.waitEligibility()
}

func (n *node) waitSent() {
	n.tracer.waitSent()
}

func (n *node) register(signer *signing.EdSigner) {
	n.hare.Register(signer)
	n.registered = append(n.registered, signer)
}

func (n *node) storeAtx(atx *types.ActivationTx) error {
	if err := atxs.Add(n.db, atx); err != nil {
		return err
	}
	n.atxsdata.AddFromAtx(atx, false)
	return nil
}

func (n *node) peerId() p2p.Peer {
	return p2p.Peer(strconv.Itoa(n.i))
}

type clusterOpt func(*lockstepCluster)

func withUnits(min, max int) clusterOpt {
	return func(cluster *lockstepCluster) {
		cluster.units.min = min
		cluster.units.max = max
	}
}

func withMockVerifier() clusterOpt {
	return func(cluster *lockstepCluster) {
		cluster.mockVerify = true
	}
}

func withMockCompactFn(f func([]byte) []byte) clusterOpt {
	return func(cluster *lockstepCluster) {
		cluster.mockCompactFn = f
	}
}

func withProposals(fraction float64) clusterOpt {
	return func(cluster *lockstepCluster) {
		cluster.proposals.fraction = fraction
		cluster.proposals.shuffle = true
	}
}

// withSigners creates N signers in addition to regular active nodes.
// this signeres will be partitioned in fair fashion across regular active nodes.
func withSigners(n int) clusterOpt {
	return func(cluster *lockstepCluster) {
		cluster.signersCount = n
	}
}

func newLockstepCluster(t *tester, opts ...clusterOpt) *lockstepCluster {
	t.Helper()
	cluster := &lockstepCluster{t: t}
	cluster.units.min = 10
	cluster.units.max = 10
	cluster.proposals.fraction = 1
	cluster.proposals.shuffle = false
	for _, opt := range opts {
		opt(cluster)
	}
	return cluster
}

// lockstepCluster allows to run rounds in lockstep
// as no peer will be able to start around until test allows it.
type lockstepCluster struct {
	t       *tester
	nodes   []*node
	signers []*node // nodes that active on consensus but don't run hare instance

	mockVerify    bool
	mockCompactFn func([]byte) []byte
	units         struct {
		min, max int
	}
	proposals struct {
		fraction float64
		shuffle  bool
	}
	signersCount int

	timestamp time.Time
}

func (cl *lockstepCluster) addNode(n *node) {
	n.hare.Start()
	cl.t.Cleanup(func() {
		n.hare.Stop()
	})
	cl.nodes = append(cl.nodes, n)
}

func (cl *lockstepCluster) partitionSigners() {
	for i, signer := range cl.signers {
		cl.nodes[i%len(cl.nodes)].register(signer.signer)
	}
}

func (cl *lockstepCluster) addSigner(n int) *lockstepCluster {
	last := len(cl.signers)
	for i := last; i < last+n; i++ {
		n := (&node{t: cl.t, i: i}).withSigner().withAtx(cl.units.min, cl.units.max)
		cl.signers = append(cl.signers, n)
	}
	return cl
}

func (cl *lockstepCluster) addActive(n int) *lockstepCluster {
	last := len(cl.nodes)
	for i := last; i < last+n; i++ {
		nn := (&node{t: cl.t, i: i}).
			withController().withSyncer().withPublisher().
			withClock().withDb().withSigner().withAtx(cl.units.min, cl.units.max).
			withOracle().withHare()
		if cl.mockVerify {
			nn = nn.withVerifier()
		}
		if cl.mockCompactFn != nil {
			nn.hare.compactFn = cl.mockCompactFn
		}
		cl.addNode(nn)
	}
	return cl
}

func (cl *lockstepCluster) addInactive(n int) *lockstepCluster {
	last := len(cl.nodes)
	for i := last; i < last+n; i++ {
		cl.addNode((&node{t: cl.t, i: i}).
			withController().withSyncer().withPublisher().
			withClock().withDb().withSigner().
			withStreamRequester().withOracle().withHare())
	}
	return cl
}

func (cl *lockstepCluster) addEquivocators(n int) *lockstepCluster {
	require.LessOrEqual(cl.t, n, len(cl.nodes))
	last := len(cl.nodes)
	for i := last; i < last+n; i++ {
		cl.addNode((&node{t: cl.t, i: i}).
			reuseSigner(cl.nodes[i-last].signer).
			withController().withSyncer().withPublisher().
			withClock().withDb().withAtx(cl.units.min, cl.units.max).
			withStreamRequester().withOracle().withHare())
	}
	return cl
}

func (cl *lockstepCluster) nogossip() {
	for _, n := range cl.nodes {
		require.NoError(cl.t, beacons.Add(n.db, cl.t.genesis.GetEpoch()+1, cl.t.beacon))
		n.mpublisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}
}

func (cl *lockstepCluster) activeSet() types.ATXIDList {
	var ids []types.ATXID
	unique := map[types.ATXID]struct{}{}
	for _, n := range append(cl.nodes, cl.signers...) {
		if n.atx == nil {
			continue
		}
		if _, exists := unique[n.atx.ID()]; exists {
			continue
		}
		unique[n.atx.ID()] = struct{}{}
		ids = append(ids, n.atx.ID())
	}
	return ids
}

func (cl *lockstepCluster) genProposalNode(lid types.LayerID, node int) {
	active := cl.activeSet()
	n := cl.nodes[node]
	if n.atx == nil {
		panic("shouldnt happen")
	}
	proposal := &types.Proposal{}
	proposal.Layer = lid
	proposal.EpochData = &types.EpochData{
		Beacon:        cl.t.beacon,
		ActiveSetHash: active.Hash(),
	}
	proposal.AtxID = n.atx.ID()
	proposal.SmesherID = n.signer.NodeID()
	id := types.ProposalID{}
	cl.t.rng.Read(id[:])
	bid := types.BallotID{}
	cl.t.rng.Read(bid[:])
	proposal.SetID(id)
	proposal.Ballot.SetID(bid)
	proposal.SetBeacon(proposal.EpochData.Beacon)
	require.NoError(cl.t, ballots.Add(n.db, &proposal.Ballot))
	n.hare.OnProposal(proposal)
}

func (cl *lockstepCluster) genProposals(lid types.LayerID, skipNodes ...int) {
	active := cl.activeSet()
	all := []*types.Proposal{}
	for i, n := range append(cl.nodes, cl.signers...) {
		if n.atx == nil || slices.Contains(skipNodes, i) {
			continue
		}
		proposal := &types.Proposal{}
		proposal.Layer = lid
		proposal.EpochData = &types.EpochData{
			Beacon:        cl.t.beacon,
			ActiveSetHash: active.Hash(),
		}
		proposal.AtxID = n.atx.ID()
		proposal.SmesherID = n.signer.NodeID()
		id := types.ProposalID{}
		cl.t.rng.Read(id[:])
		bid := types.BallotID{}
		cl.t.rng.Read(bid[:])
		proposal.SetID(id)
		proposal.Ballot.SetID(bid)
		proposal.SetBeacon(proposal.EpochData.Beacon)
		all = append(all, proposal)
	}
	for _, other := range cl.nodes {
		cp := make([]*types.Proposal, len(all))
		copy(cp, all)
		if cl.proposals.shuffle {
			cl.t.rng.Shuffle(len(cp), func(i, j int) {
				cp[i], cp[j] = cp[j], cp[i]
			})
		}
		for _, proposal := range cp[:int(float64(len(cp))*cl.proposals.fraction)] {
			require.NoError(cl.t, ballots.Add(other.db, &proposal.Ballot))
			other.hare.OnProposal(proposal)
		}
	}
}

func (cl *lockstepCluster) setup() {
	active := cl.activeSet()
	for _, n := range cl.nodes {
		require.NoError(cl.t, beacons.Add(n.db, cl.t.genesis.GetEpoch()+1, cl.t.beacon))
		for _, other := range append(cl.nodes, cl.signers...) {
			if other.atx == nil {
				continue
			}
			require.NoError(cl.t, n.storeAtx(other.atx))
		}
		n.oracle.UpdateActiveSet(cl.t.genesis.GetEpoch()+1, active)
		n.mpublisher.EXPECT().
			Publish(gomock.Any(), gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, _ string, msg []byte) error {
				for _, other := range cl.nodes {
					other.hare.Handler(ctx, n.peerId(), msg)
				}
				return nil
			}).
			AnyTimes()
	}
}

func (cl *lockstepCluster) movePreround(layer types.LayerID) {
	cl.timestamp = cl.t.start.
		Add(cl.t.layerDuration * time.Duration(layer)).
		Add(cl.t.cfg.PreroundDelay)
	for _, n := range cl.nodes {
		n.nclock.StartLayer(layer)
		n.clock.Advance(cl.timestamp.Sub(n.clock.Now()))
	}
	for _, n := range cl.nodes {
		n.waitEligibility()
	}
	for _, n := range cl.nodes {
		n.waitSent()
	}
}

func (cl *lockstepCluster) moveRound() {
	cl.timestamp = cl.timestamp.Add(cl.t.cfg.RoundDuration)
	for _, n := range cl.nodes {
		n.clock.Advance(cl.timestamp.Sub(n.clock.Now()))
	}
	for _, n := range cl.nodes {
		n.waitEligibility()
	}
	for _, n := range cl.nodes {
		n.waitSent()
	}
}

func (cl *lockstepCluster) waitStopped() {
	for _, n := range cl.nodes {
		n.tracer.waitStopped()
	}
}

func newTestTracer(tb testing.TB) *testTracer {
	return &testTracer{
		TB:          tb,
		stopped:     make(chan types.LayerID, 100),
		eligibility: make(chan []*types.HareEligibility),
		sent:        make(chan *Message),
	}
}

type testTracer struct {
	testing.TB
	stopped     chan types.LayerID
	eligibility chan []*types.HareEligibility
	sent        chan *Message
}

func (t *testTracer) waitStopped() types.LayerID {
	wait := 10 * time.Second
	select {
	case <-time.After(wait):
		require.FailNow(t, "didn't stop", "wait %v", wait)
	case lid := <-t.stopped:
		return lid
	}
	return 0
}

func (t *testTracer) waitEligibility() []*types.HareEligibility {
	wait := 10 * time.Second
	select {
	case <-time.After(wait):
		require.FailNow(t, "no eligibility", "wait %v", wait)
	case el := <-t.eligibility:
		return el
	}
	return nil
}

func (t *testTracer) waitSent() *Message {
	wait := 10 * time.Second
	select {
	case <-time.After(wait):
		require.FailNow(t, "no message", "wait %v", wait)
	case m := <-t.sent:
		return m
	}
	return nil
}

func (*testTracer) OnStart(types.LayerID) {}

func (t *testTracer) OnStop(lid types.LayerID) {
	select {
	case t.stopped <- lid:
	default:
	}
}

func (t *testTracer) OnActive(el []*types.HareEligibility) {
	wait := 10 * time.Second
	select {
	case <-time.After(wait):
		require.FailNow(t, "eligibility can't be sent", "wait %v", wait)
	case t.eligibility <- el:
	}
}

func (t *testTracer) OnMessageSent(m *Message) {
	wait := 10 * time.Second
	select {
	case <-time.After(wait):
		require.FailNow(t, "message can't be sent", "wait %v", wait)
	case t.sent <- m:
	}
}

func (*testTracer) OnMessageReceived(*Message) {}

func testHare(t *testing.T, active, inactive, equivocators int, opts ...clusterOpt) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.LogStats = true
	tst := &tester{
		TB:            t,
		rng:           rand.New(rand.NewSource(1001)),
		start:         time.Now(),
		cfg:           cfg,
		layerDuration: 5 * time.Minute,
		beacon:        types.Beacon{1, 1, 1, 1},
		genesis:       types.GetEffectiveGenesis(),
	}
	cluster := newLockstepCluster(tst, opts...).
		addActive(active).
		addInactive(inactive).
		addEquivocators(equivocators)
	if cluster.signersCount > 0 {
		cluster = cluster.addSigner(cluster.signersCount)
		cluster.partitionSigners()
	}

	layer := tst.genesis + 1
	cluster.setup()
	cluster.genProposals(layer)
	cluster.movePreround(layer)
	for i := 0; i < 2*int(notify); i++ {
		cluster.moveRound()
	}
	var consistent []types.ProposalID
	cluster.waitStopped()
	for _, n := range cluster.nodes {
		select {
		case coin := <-n.hare.Coins():
			require.Equal(t, coin.Layer, layer)
		default:
			require.FailNow(t, "no coin")
		}
		select {
		case rst := <-n.hare.Results():
			require.Equal(t, rst.Layer, layer)
			require.NotEmpty(t, rst.Proposals)
			if consistent == nil {
				consistent = rst.Proposals
			} else {
				require.Equal(t, consistent, rst.Proposals)
			}
		default:
			require.FailNow(t, "no result")
		}
		require.Empty(t, n.hare.Running())
	}
}

func TestHare(t *testing.T) {
	t.Run("one", func(t *testing.T) { testHare(t, 1, 0, 0) })
	t.Run("two", func(t *testing.T) { testHare(t, 2, 0, 0) })
	t.Run("small", func(t *testing.T) { testHare(t, 5, 0, 0) })
	t.Run("with proposals subsets", func(t *testing.T) {
		t.Skip("this test needs to be rewritten or removed")
		testHare(t, 5, 0, 0, withProposals(0.5))
	})
	t.Run("with units", func(t *testing.T) { testHare(t, 5, 0, 0, withUnits(10, 50)) })
	t.Run("with inactive", func(t *testing.T) { testHare(t, 3, 2, 0) })
	t.Run("equivocators", func(t *testing.T) {
		t.Skip("this test needs to be rewritten or removed")
		testHare(t, 4, 0, 1, withProposals(0.75))
	})
	t.Run("one active multi signers", func(t *testing.T) { testHare(t, 1, 0, 0, withSigners(2)) })
	t.Run("three active multi signers", func(t *testing.T) { testHare(t, 3, 0, 0, withSigners(10)) })
}

func TestIterationLimit(t *testing.T) {
	t.Parallel()
	tst := &tester{
		TB:            t,
		rng:           rand.New(rand.NewSource(1001)),
		start:         time.Now(),
		cfg:           DefaultConfig(),
		layerDuration: 5 * time.Minute,
		beacon:        types.Beacon{1, 1, 1, 1},
		genesis:       types.GetEffectiveGenesis(),
	}
	tst.cfg.IterationsLimit = 3

	layer := tst.genesis + 1
	cluster := newLockstepCluster(tst)
	cluster.addActive(1)
	cluster.nogossip()
	cluster.movePreround(layer)
	for i := 0; i < int(tst.cfg.IterationsLimit)*int(notify); i++ {
		cluster.moveRound()
	}
	cluster.waitStopped()
	require.Empty(t, cluster.nodes[0].hare.Running())
	require.False(t, cluster.nodes[0].patrol.IsHareInCharge(layer))
}

func TestConfigMarshal(t *testing.T) {
	enc := zapcore.NewMapObjectEncoder()
	cfg := &Config{}
	require.NoError(t, cfg.MarshalLogObject(enc))
}

func TestHandler(t *testing.T) {
	t.Parallel()
	tst := &tester{
		TB:            t,
		rng:           rand.New(rand.NewSource(1001)),
		start:         time.Now(),
		cfg:           DefaultConfig(),
		layerDuration: 5 * time.Minute,
		beacon:        types.Beacon{1, 1, 1, 1},
		genesis:       types.GetEffectiveGenesis(),
	}
	cluster := newLockstepCluster(tst)
	cluster.addActive(1)
	n := cluster.nodes[0]
	require.NoError(t, beacons.Add(n.db, tst.genesis.GetEpoch()+1, tst.beacon))
	require.NoError(t, n.storeAtx(n.atx))
	n.oracle.UpdateActiveSet(tst.genesis.GetEpoch()+1, []types.ATXID{n.atx.ID()})
	n.mpublisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	layer := tst.genesis + 1
	n.nclock.StartLayer(layer)
	n.clock.Advance((tst.start.
		Add(tst.layerDuration * time.Duration(layer)).
		Add(tst.cfg.PreroundDelay)).Sub(n.clock.Now()))
	elig := n.tracer.waitEligibility()[0]

	n.tracer.waitSent()
	n.tracer.waitEligibility()

	t.Run("malformed", func(t *testing.T) {
		require.ErrorIs(t, n.hare.Handler(context.Background(), "", []byte("malformed")),
			pubsub.ErrValidationReject)
		require.ErrorContains(t, n.hare.Handler(context.Background(), "", []byte("malformed")),
			"decoding")
	})
	t.Run("invalidated", func(t *testing.T) {
		msg := &Message{}
		msg.Round = commit
		require.ErrorIs(t, n.hare.Handler(context.Background(), "", codec.MustEncode(msg)),
			pubsub.ErrValidationReject)
		require.ErrorContains(t, n.hare.Handler(context.Background(), "", codec.MustEncode(msg)),
			"validation reference")
	})
	t.Run("unregistered", func(t *testing.T) {
		msg := &Message{}
		require.ErrorContains(t, n.hare.Handler(context.Background(), "", codec.MustEncode(msg)),
			"is not registered")
	})
	t.Run("invalid signature", func(t *testing.T) {
		msg := &Message{}
		msg.Body.IterRound.Round = propose
		msg.Layer = layer
		msg.Sender = n.signer.NodeID()
		msg.Signature = n.signer.Sign(signing.HARE+1, msg.ToMetadata().ToBytes())
		require.ErrorIs(t, n.hare.Handler(context.Background(), "", codec.MustEncode(msg)),
			pubsub.ErrValidationReject)
		require.ErrorContains(t, n.hare.Handler(context.Background(), "", codec.MustEncode(msg)),
			"invalid signature")
	})
	t.Run("zero grade", func(t *testing.T) {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		msg := &Message{}
		msg.Body.IterRound.Round = propose
		msg.Layer = layer
		msg.Sender = signer.NodeID()
		msg.Signature = signer.Sign(signing.HARE, msg.ToMetadata().ToBytes())
		require.ErrorContains(t, n.hare.Handler(context.Background(), "", codec.MustEncode(msg)),
			"zero grade")
	})
	t.Run("equivocation", func(t *testing.T) {
		b := types.RandomBallot()
		b.InnerBallot.Layer = layer
		b.Layer = layer
		p1 := &types.Proposal{
			InnerProposal: types.InnerProposal{
				Ballot: *b,
				TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
			},
		}
		b2 := types.RandomBallot()

		b2.InnerBallot.Layer = layer
		b.Layer = layer
		p2 := &types.Proposal{
			InnerProposal: types.InnerProposal{
				Ballot: *b2,
				TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
			},
		}

		p1.Initialize()
		p2.Initialize()

		if err := n.hare.OnProposal(p1); err != nil {
			panic(err)
		}

		if err := n.hare.OnProposal(p2); err != nil {
			panic(err)
		}

		msg1 := &Message{}
		msg1.Layer = layer
		msg1.Value.Proposals = []types.ProposalID{p1.ID()}
		msg1.Eligibility = *elig
		msg1.Sender = n.signer.NodeID()
		msg1.Signature = n.signer.Sign(signing.HARE, msg1.ToMetadata().ToBytes())
		msg1.Value.Proposals = nil
		msg1.Value.CompactProposals = []types.CompactProposalID{compactProposal(compactTruncate, p1.ID())}

		msg2 := &Message{}
		msg2.Layer = layer
		msg2.Value.Proposals = []types.ProposalID{p2.ID()}
		msg2.Eligibility = *elig
		msg2.Sender = n.signer.NodeID()
		msg2.Signature = n.signer.Sign(signing.HARE, msg2.ToMetadata().ToBytes())
		msg2.Value.Proposals = nil
		msg2.Value.CompactProposals = []types.CompactProposalID{compactProposal(compactTruncate, p2.ID())}

		require.NoError(t, n.hare.Handler(context.Background(), "", codec.MustEncode(msg1)))
		require.NoError(t, n.hare.Handler(context.Background(), "", codec.MustEncode(msg2)))

		malicious, err := identities.IsMalicious(n.db, n.signer.NodeID())
		require.NoError(t, err)
		require.True(t, malicious)

		require.ErrorContains(t,
			n.hare.Handler(context.Background(), "", codec.MustEncode(msg2)),
			"dropped by graded",
		)
	})
}

func gatx(id types.ATXID, epoch types.EpochID, smesher types.NodeID, base, height uint64) types.ActivationTx {
	atx := &types.ActivationTx{
		NumUnits:       10,
		PublishEpoch:   epoch,
		VRFNonce:       1,
		BaseTickHeight: base,
		TickCount:      height - base,
		SmesherID:      smesher,
	}
	atx.SetID(id)
	atx.SetReceived(time.Time{}.Add(1))
	return *atx
}

func gproposal(
	id types.ProposalID,
	atxid types.ATXID,
	smesher types.NodeID,
	layer types.LayerID,
	beacon types.Beacon,
) *types.Proposal {
	proposal := types.Proposal{}
	proposal.Layer = layer
	proposal.EpochData = &types.EpochData{
		Beacon: beacon,
	}
	proposal.AtxID = atxid
	proposal.SmesherID = smesher
	proposal.Ballot.SmesherID = smesher
	proposal.SetID(id)
	proposal.Ballot.SetID(types.BallotID(id))
	proposal.SetBeacon(beacon)
	return &proposal
}

func TestProposals(t *testing.T) {
	atxids := [3]types.ATXID{}
	pids := [3]types.ProposalID{}
	ids := [3]types.NodeID{}
	for i := range atxids {
		atxids[i][0] = byte(i) + 1
		pids[i][0] = byte(i) + 1
		ids[i][0] = byte(i) + 1
	}
	// t.Fatal(atxids)
	publish := types.EpochID(1)
	layer := (publish + 1).FirstLayer()
	goodBeacon := types.Beacon{1}
	badBeacon := types.Beacon{2}

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	for _, tc := range []struct {
		desc      string
		atxs      []types.ActivationTx
		proposals []*types.Proposal
		malicious []types.NodeID
		layer     types.LayerID
		beacon    types.Beacon
		expect    []types.ProposalID
	}{
		{
			desc:   "sanity",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.ActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
				gatx(atxids[2], publish, signer.NodeID(), 10, 100),
			},
			proposals: []*types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gproposal(pids[1], atxids[1], ids[1], layer, goodBeacon),
			},
			expect: []types.ProposalID{pids[0], pids[1]},
		},
		{
			desc:   "mismatched beacon",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.ActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
				gatx(atxids[2], publish, signer.NodeID(), 10, 100),
			},
			proposals: []*types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gproposal(pids[1], atxids[1], ids[1], layer, badBeacon),
			},
			expect: []types.ProposalID{pids[0]},
		},
		{
			desc:   "multiproposals",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.ActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
				gatx(atxids[2], publish, signer.NodeID(), 10, 100),
			},
			proposals: []*types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gproposal(pids[1], atxids[1], ids[1], layer, goodBeacon),
				gproposal(pids[2], atxids[1], ids[1], layer, goodBeacon),
			},
			expect: []types.ProposalID{pids[0]},
		},
		{
			desc:   "future proposal",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.ActivationTx{
				gatx(atxids[0], publish, ids[0], 101, 1000),
				gatx(atxids[1], publish, signer.NodeID(), 10, 100),
			},
			proposals: []*types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gproposal(pids[1], atxids[1], ids[1], layer, goodBeacon),
			},
			expect: []types.ProposalID{pids[1]},
		},
		{
			desc:   "malicious",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.ActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
				gatx(atxids[2], publish, signer.NodeID(), 10, 100),
			},
			proposals: []*types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gproposal(pids[1], atxids[1], ids[1], layer, goodBeacon),
			},
			malicious: []types.NodeID{ids[0]},
			expect:    []types.ProposalID{pids[1]},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			db := sql.InMemory()
			atxsdata := atxsdata.New()
			proposals := store.New()
			hare := New(
				nil,
				nil,
				db,
				atxsdata,
				proposals,
				signing.NewEdVerifier(),
				nil,
				nil,
				layerpatrol.New(),
				WithLogger(logtest.New(t).Zap()),
			)
			for _, atx := range tc.atxs {
				require.NoError(t, atxs.Add(db, &atx))
				atxsdata.AddFromAtx(&atx, false)
			}
			for _, proposal := range tc.proposals {
				if err := proposals.Add(proposal); err != nil {
					panic(err)
				}
			}
			for _, id := range tc.malicious {
				require.NoError(t, identities.SetMalicious(db, id, []byte("non empty"), time.Time{}))
				atxsdata.SetMalicious(id)
			}
			require.ElementsMatch(t, tc.expect, hare.selectProposals(&session{
				lid:     tc.layer,
				beacon:  tc.beacon,
				signers: []*signing.EdSigner{signer},
			}))
		})
	}
}

func TestHare_AddProposal(t *testing.T) {
	t.Parallel()
	proposals := store.New()
	hare := New(nil, nil, nil, nil, proposals, nil, nil, nil, nil)

	p := gproposal(
		types.RandomProposalID(),
		types.RandomATXID(),
		types.RandomNodeID(),
		types.LayerID(0),
		types.RandomBeacon(),
	)
	require.False(t, hare.IsKnown(p.Layer, p.ID()))
	require.NoError(t, hare.OnProposal(p))
	require.True(t, proposals.Has(p.ID()))

	require.True(t, hare.IsKnown(p.Layer, p.ID()))
	require.ErrorIs(t, hare.OnProposal(p), store.ErrProposalExists)
}

func TestProposalIDSort(t *testing.T) {
	var (
		a = types.ProposalID{0, 3, 2, 3, 5}
		b = types.ProposalID{0, 1, 2, 3, 4}
		c = types.ProposalID{11, 4, 6, 254, 0}
		d = types.ProposalID{0, 1, 2, 3, 5}
	)
	srt := []types.ProposalID{c, b, a, d}
	slices.SortFunc(srt, sortProposalIds)
	require.Equal(t, []types.ProposalID{b, d, a, c}, srt)
}

// TestHare_ReconstructForward tests that a message
// could be reconstructed on a downstream peer that
// receives a gossipsub message from a node.
func TestHare_Reconstruct(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LogStats = true
	tst := &tester{
		TB:            t,
		rng:           rand.New(rand.NewSource(1001)),
		start:         time.Now(),
		cfg:           cfg,
		layerDuration: 5 * time.Minute,
		beacon:        types.Beacon{1, 1, 1, 1},
		genesis:       types.GetEffectiveGenesis(),
	}
	cluster := newLockstepCluster(tst).
		addActive(3)
	if cluster.signersCount > 0 {
		cluster = cluster.addSigner(cluster.signersCount)
		cluster.partitionSigners()
	}

	layer := tst.genesis + 1

	cluster.setup()

	cluster.genProposals(layer, 2)
	cluster.genProposalNode(layer, 2)
	cluster.movePreround(layer)
	for i := 0; i < 2*int(notify); i++ {
		cluster.moveRound()
	}
	var consistent []types.ProposalID
	cluster.waitStopped()
	for _, n := range cluster.nodes {
		select {
		case coin := <-n.hare.Coins():
			require.Equal(t, coin.Layer, layer)
		default:
			require.FailNow(t, "no coin")
		}
		select {
		case rst := <-n.hare.Results():
			require.Equal(t, rst.Layer, layer)
			require.NotEmpty(t, rst.Proposals)
			if consistent == nil {
				consistent = rst.Proposals
			} else {
				require.Equal(t, consistent, rst.Proposals)
			}
		default:
			require.FailNow(t, "no result")
		}
		require.Empty(t, n.hare.Running())
	}
}

// TestHare_ReconstructAll tests that the nodes go into a
// full message exchange in the case that a signature fails
// although all compact hashes and proposals match.
func TestHare_ReconstructAll(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LogStats = true
	tst := &tester{
		TB:            t,
		rng:           rand.New(rand.NewSource(1001)),
		start:         time.Now(),
		cfg:           cfg,
		layerDuration: 5 * time.Minute,
		beacon:        types.Beacon{1, 1, 1, 1},
		genesis:       types.GetEffectiveGenesis(),
	}
	cluster := newLockstepCluster(tst, withMockVerifier()).
		addActive(3)
	if cluster.signersCount > 0 {
		cluster = cluster.addSigner(cluster.signersCount)
		cluster.partitionSigners()
	}
	layer := tst.genesis + 1

	// cluster setup
	active := cluster.activeSet()
	calls := [3]int{}
	for i, n := range cluster.nodes {
		n.mverifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ signing.Domain, _ types.NodeID, _ []byte, _ types.EdSignature) bool {
				calls[i] = calls[i] + 1
				// when first call return false, otherwise true
				return !(calls[i] == 1)
			}).AnyTimes()
	}
	for _, n := range cluster.nodes {
		require.NoError(cluster.t, beacons.Add(n.db, cluster.t.genesis.GetEpoch()+1, cluster.t.beacon))
		for _, other := range append(cluster.nodes, cluster.signers...) {
			if other.atx == nil {
				continue
			}
			require.NoError(cluster.t, n.storeAtx(other.atx))
		}
		n.oracle.UpdateActiveSet(cluster.t.genesis.GetEpoch()+1, active)
		n.mpublisher.EXPECT().
			Publish(gomock.Any(), gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, proto string, msg []byte) error {
				for _, other := range cluster.nodes {
					if err := other.hare.Handler(ctx, n.peerId(), msg); err != nil {
						panic(err)
					}
				}
				return nil
			}).
			AnyTimes()
	}

	cluster.genProposals(layer)
	cluster.movePreround(layer)
	for i := 0; i < 2*int(notify); i++ {
		cluster.moveRound()
	}
	var consistent []types.ProposalID
	cluster.waitStopped()
	for _, n := range cluster.nodes {
		select {
		case coin := <-n.hare.Coins():
			require.Equal(t, coin.Layer, layer)
		default:
			require.FailNow(t, "no coin")
		}
		select {
		case rst := <-n.hare.Results():
			require.Equal(t, rst.Layer, layer)
			require.NotEmpty(t, rst.Proposals)
			if consistent == nil {
				consistent = rst.Proposals
			} else {
				require.Equal(t, consistent, rst.Proposals)
			}
		default:
			t.Fatal("no result")
		}
		require.Empty(t, n.hare.Running())
	}
}

// TestHare_ReconstructCollision tests the behavior during a collision.
func TestHare_ReconstructCollision(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LogStats = true
	tst := &tester{
		TB:            t,
		rng:           rand.New(rand.NewSource(1000)),
		start:         time.Now(),
		cfg:           cfg,
		layerDuration: 5 * time.Minute,
		beacon:        types.Beacon{1, 1, 1, 1},
		genesis:       types.GetEffectiveGenesis(),
	}

	fn := func(_ []byte) []byte {
		return []byte{0x11, 0x11, 0x11, 0x11}
	}
	cluster := newLockstepCluster(tst, withMockCompactFn(fn), withSigners(2), withProposals(1)).
		addActive(2)
	if cluster.signersCount > 0 {
		cluster = cluster.addSigner(cluster.signersCount)
		cluster.partitionSigners()
	}
	layer := tst.genesis + 1

	// scenario:
	// node 1 has generated 1 proposal that (mocked) hash into 0x0 as prefix - both nodes know the proposal
	// node 2 has generated 1 proposal that hash into 0x0 (but node 1 doesn't know about it)
	// so the two proposals collide and then we check that the nodes actually go into a round of
	// exchanging the missing/colliding hashes and then the signature verification (not mocked)
	// should pass and that a full exchange of all hashes is not triggered (disambiguates case of
	// failed signature vs. hashes colliding - there's a difference in number of single prefixes
	// that are sent, but the response should be the same)
	cluster.setup()
	cluster.genProposals(layer, 1)
	cluster.genProposalNode(layer, 1)
	cluster.movePreround(layer)
	for i := 0; i < 2*int(notify); i++ {
		cluster.moveRound()
	}
	var consistent []types.ProposalID
	cluster.waitStopped()
	for _, n := range cluster.nodes {
		select {
		case coin := <-n.hare.Coins():
			require.Equal(t, coin.Layer, layer)
		default:
			require.FailNow(t, "no coin")
		}
		select {
		case rst := <-n.hare.Results():
			require.Equal(t, rst.Layer, layer)
			require.NotEmpty(t, rst.Proposals)
			if consistent == nil {
				consistent = rst.Proposals
			} else {
				require.Equal(t, consistent, rst.Proposals)
			}
		default:
			t.Fatal("no result")
		}
		require.Empty(t, n.hare.Running())
	}
}
