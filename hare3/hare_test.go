package hare3

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/timesync"
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

func newNode(t *tester, i int) *node {
	n := &node{t: t, i: i}
	n = n.
		withController().withSyncer().withPublisher().
		withClock().withDb().withSigner().withAtx().withAtx().
		withOracle().withHare()
	n.hare.Start()
	t.Cleanup(func() {
		n.hare.Stop()
	})
	return n
}

type node struct {
	t *tester

	i         int
	clock     *clock.Mock
	signer    *signing.EdSigner
	vrfsigner *signing.VRFSigner
	atx       *types.VerifiedActivationTx
	oracle    *eligibility.Oracle
	db        *datastore.CachedDB

	ctrl       *gomock.Controller
	mpublisher *pmocks.MockPublishSubsciber
	msyncer    *smocks.MockSyncStateProvider

	tracer *testTracer
	hare   *Hare
}

func (n *node) withClock() *node {
	n.clock = clock.NewMock()
	n.clock.Set(n.t.start)
	return n
}

func (n *node) withSigner() *node {
	signer, err := signing.NewEdSigner(signing.WithKeyFromRand(n.t.rng))
	require.NoError(n.t, err)
	n.signer = signer
	vrfsigner, err := signer.VRFSigner()
	require.NoError(n.t, err)
	n.vrfsigner = vrfsigner
	return n
}

func (n *node) withDb() *node {
	n.db = datastore.NewCachedDB(sql.InMemory(), log.NewNop())
	return n
}

func (n *node) withAtx() *node {
	atx := &types.ActivationTx{}
	atx.NumUnits = 10
	atx.PublishEpoch = n.t.genesis.GetEpoch()
	atx.SmesherID = n.signer.NodeID()
	id := types.ATXID{}
	n.t.rng.Read(id[:])
	atx.SetID(id)
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(n.t.start)
	nonce := types.VRFPostIndex(n.t.rng.Uint64())
	atx.VRFNonce = &nonce
	verified, err := atx.Verify(0, 100)
	require.NoError(n.t, err)
	n.atx = verified
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

func (n *node) withOracle() *node {
	beaconget := smocks.NewMockBeaconGetter(n.ctrl)
	beaconget.EXPECT().GetBeacon(gomock.Any()).DoAndReturn(func(epoch types.EpochID) (types.Beacon, error) {
		return beacons.Get(n.db, epoch)
	}).AnyTimes()
	n.oracle = eligibility.New(beaconget, n.db, signing.NewVRFVerifier(), n.vrfsigner, layersPerEpoch, config.DefaultConfig(), log.NewNop())
	return n
}

func (n *node) withPublisher() *node {
	n.mpublisher = pmocks.NewMockPublishSubsciber(n.ctrl)
	n.mpublisher.EXPECT().Register(gomock.Any(), gomock.Any()).AnyTimes()
	return n
}

func (n *node) withHare() *node {
	logger := logtest.New(n.t).Named(fmt.Sprintf("hare=%d", n.i))
	verifier, err := signing.NewEdVerifier()
	require.NoError(n.t, err)
	nodeclock, err := timesync.NewClock(
		timesync.WithLogger(log.NewNop()),
		timesync.WithClock(n.clock),
		timesync.WithGenesisTime(n.t.start),
		timesync.WithLayerDuration(n.t.layerDuration),
		timesync.WithTickInterval(n.t.layerDuration),
	)
	require.NoError(n.t, err)

	tracer := newTestTracer(n.t)
	n.tracer = tracer
	n.hare = New(nodeclock, n.mpublisher, n.db, verifier, n.signer, n.oracle, n.msyncer,
		WithConfig(n.t.cfg),
		WithLogger(logger.Zap()),
		WithWallclock(n.clock),
		WithEnableLayer(n.t.genesis),
		WithTracer(tracer),
	)
	return n
}

// lockstepCluster allows to run rounds in lockstep
// as no peer will be able to start around until test allows it.
type lockstepCluster struct {
	t     *tester
	nodes []*node

	timestamp time.Time
	start     chan struct{}
	complete  chan struct{}
}

func (cl *lockstepCluster) nogossip() {
	cl.start = make(chan struct{}, len(cl.nodes))
	cl.complete = make(chan struct{}, len(cl.nodes))
	for _, n := range cl.nodes {
		require.NoError(cl.t, beacons.Add(n.db, cl.t.genesis.GetEpoch()+1, cl.t.beacon))
		n.mpublisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Do(func(ctx context.Context, _ string, msg []byte) error {
			cl.timedReceive(cl.start)
			cl.timedSend(cl.complete)
			return nil
		}).AnyTimes()
	}
}

func (cl *lockstepCluster) setup(ids []types.ATXID) {
	cl.start = make(chan struct{}, len(cl.nodes))
	cl.complete = make(chan struct{}, len(cl.nodes))
	for _, n := range cl.nodes {
		require.NoError(cl.t, beacons.Add(n.db, cl.t.genesis.GetEpoch()+1, cl.t.beacon))
		for _, other := range cl.nodes {
			require.NoError(cl.t, atxs.Add(n.db, other.atx))
		}
		n.oracle.UpdateActiveSet(cl.t.genesis.GetEpoch()+1, ids)
		n.mpublisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Do(func(ctx context.Context, _ string, msg []byte) error {
			cl.timedReceive(cl.start)
			var eg errgroup.Group
			for _, other := range cl.nodes {
				other := other
				eg.Go(func() error {
					return other.hare.Handler(ctx, "self", msg)
				})
			}
			err := eg.Wait()
			cl.timedSend(cl.complete)
			return err
		}).AnyTimes()
	}
}

func (cl *lockstepCluster) movePreround(layer types.LayerID) {
	cl.timestamp = cl.t.start.
		Add(cl.t.layerDuration * time.Duration(layer)).
		Add(cl.t.cfg.PreroundDelay)
	for _, n := range cl.nodes {
		n.clock.Set(cl.timestamp)
	}
	send := 0
	for _, n := range cl.nodes {
		if n.tracer.waitEligibility() != nil {
			send++
		}
	}
	for i := 0; i < send; i++ {
		cl.timedSend(cl.start)
	}
	for i := 0; i < send; i++ {
		cl.timedReceive(cl.complete)
	}
}

func (cl *lockstepCluster) moveRound() {
	cl.timestamp = cl.timestamp.Add(cl.t.cfg.RoundDuration)
	send := 0
	for _, n := range cl.nodes {
		n.clock.Set(cl.timestamp)
	}
	for _, n := range cl.nodes {
		if n.tracer.waitEligibility() != nil {
			send++
		}
	}
	for i := 0; i < send; i++ {
		cl.timedSend(cl.start)
	}
	for i := 0; i < send; i++ {
		cl.timedReceive(cl.complete)
	}
}

func (cl *lockstepCluster) timedSend(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	case <-time.After(time.Second):
		require.FailNow(cl.t, "send timed out")
	}
}

func (cl *lockstepCluster) timedReceive(ch chan struct{}) {
	select {
	case <-ch:
	case <-time.After(time.Second):
		require.FailNow(cl.t, "receive timed out")
	}
}

func newTestTracer(tb testing.TB) *testTracer {
	return &testTracer{
		TB:          tb,
		stopped:     make(chan types.LayerID, 100),
		eligibility: make(chan *types.HareEligibility, 100),
	}
}

type testTracer struct {
	testing.TB
	stopped     chan types.LayerID
	eligibility chan *types.HareEligibility
}

func (t *testTracer) waitStopped() types.LayerID {
	wait := time.Second
	select {
	case <-time.After(wait):
		require.FailNow(t, "didn't stop", "wait %v", wait)
	case lid := <-t.stopped:
		return lid
	}
	return 0
}

func (t *testTracer) waitEligibility() *types.HareEligibility {
	wait := time.Second
	select {
	case <-time.After(wait):
		require.FailNow(t, "no eligibility", "wait %v", wait)
	case el := <-t.eligibility:
		return el
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

func (t *testTracer) OnActive(el *types.HareEligibility) {
	select {
	case t.eligibility <- el:
	default:
	}
}

func (*testTracer) OnMessageSent(*Message) {}

func (*testTracer) OnMessageReceived(*Message) {}

func testHare(tb testing.TB, n int) {
	tb.Helper()

	tst := &tester{
		TB:            tb,
		rng:           rand.New(rand.NewSource(1001)),
		start:         time.Now(),
		cfg:           DefaultConfig(),
		layerDuration: 5 * time.Minute,
		beacon:        types.Beacon{1, 1, 1, 1},
		genesis:       types.GetEffectiveGenesis(),
	}
	nodes := make([]*node, n)
	ids := make([]types.ATXID, n)
	for i := range nodes {
		nodes[i] = newNode(tst, i)
		ids[i] = nodes[i].atx.ID()
	}
	layer := tst.genesis + 1
	maxProposals := n
	if maxProposals > 50 {
		maxProposals = 50
	}
	for _, n := range nodes[:maxProposals] {
		proposal := &types.Proposal{}
		proposal.Layer = layer
		proposal.ActiveSet = ids
		proposal.EpochData = &types.EpochData{
			Beacon: tst.beacon,
		}
		proposal.AtxID = n.atx.ID()
		proposal.SmesherID = n.signer.NodeID()
		id := types.ProposalID{}
		tst.rng.Read(id[:])
		bid := types.BallotID{}
		tst.rng.Read(bid[:])
		proposal.SetID(id)
		proposal.Ballot.SetID(bid)
		for _, other := range nodes {
			require.NoError(tb, ballots.Add(other.db, &proposal.Ballot))
			require.NoError(tb, proposals.Add(other.db, proposal))
		}
	}
	cluster := lockstepCluster{
		t:     tst,
		nodes: nodes,
	}
	cluster.setup(ids)
	cluster.movePreround(layer)
	for i := 0; i < 2*int(notify); i++ {
		cluster.moveRound()
	}
	for _, n := range nodes {
		n.tracer.waitStopped()
		select {
		case coin := <-n.hare.Coins():
			require.Equal(tb, coin.Layer, layer)
		default:
			require.FailNow(tb, "no coin")
		}
		select {
		case rst := <-n.hare.Results():
			require.Equal(tb, rst.Layer, layer)
			require.NotEmpty(tb, rst.Proposals)
		default:
			require.FailNow(tb, "no result")
		}
		require.Empty(tb, n.hare.Running())
	}
}

func TestHare(t *testing.T) {
	t.Run("one", func(t *testing.T) { testHare(t, 1) })
	t.Run("two", func(t *testing.T) { testHare(t, 2) })
	t.Run("small", func(t *testing.T) { testHare(t, 5) })
}

func TestIterationLimit(t *testing.T) {
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

	n := newNode(tst, 0)
	layer := tst.genesis + 1
	cluster := lockstepCluster{
		t:     tst,
		nodes: []*node{n},
	}
	cluster.nogossip()
	cluster.movePreround(layer)
	for i := 0; i < int(tst.cfg.IterationsLimit)*int(notify)-1; i++ {
		cluster.moveRound()
	}
	require.Empty(t, n.hare.Running())
}

func TestConfigMarshal(t *testing.T) {
	enc := zapcore.NewMapObjectEncoder()
	cfg := &Config{}
	require.NoError(t, cfg.MarshalLogObject(enc))
}

func TestHandler(t *testing.T) {
	tst := &tester{
		TB:            t,
		rng:           rand.New(rand.NewSource(1001)),
		start:         time.Now(),
		cfg:           DefaultConfig(),
		layerDuration: 5 * time.Minute,
		beacon:        types.Beacon{1, 1, 1, 1},
		genesis:       types.GetEffectiveGenesis(),
	}
	n := newNode(tst, 0)
	require.NoError(t, beacons.Add(n.db, tst.genesis.GetEpoch()+1, tst.beacon))
	require.NoError(t, atxs.Add(n.db, n.atx))
	n.oracle.UpdateActiveSet(tst.genesis.GetEpoch()+1, []types.ATXID{n.atx.ID()})
	n.mpublisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	layer := tst.genesis + 1
	n.clock.Set(tst.start.
		Add(tst.layerDuration * time.Duration(layer)).
		Add(tst.cfg.PreroundDelay),
	)
	elig := n.tracer.waitEligibility()
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
		msg.Layer = layer
		msg.Sender = signer.NodeID()
		msg.Signature = signer.Sign(signing.HARE, msg.ToMetadata().ToBytes())
		require.ErrorContains(t, n.hare.Handler(context.Background(), "", codec.MustEncode(msg)),
			"zero grade")
	})
	t.Run("equivocation", func(t *testing.T) {
		msg1 := &Message{}
		msg1.Layer = layer
		msg1.Value.Proposals = []types.ProposalID{{1}}
		msg1.Eligibility = *elig
		msg1.Sender = n.signer.NodeID()
		msg1.Signature = n.signer.Sign(signing.HARE, msg1.ToMetadata().ToBytes())

		msg2 := &Message{}
		msg2.Layer = layer
		msg2.Value.Proposals = []types.ProposalID{{2}}
		msg2.Eligibility = *elig
		msg2.Sender = n.signer.NodeID()
		msg2.Signature = n.signer.Sign(signing.HARE, msg2.ToMetadata().ToBytes())

		require.NoError(t, n.hare.Handler(context.Background(), "", codec.MustEncode(msg1)))
		require.NoError(t, n.hare.Handler(context.Background(), "", codec.MustEncode(msg2)))

		malicious, err := n.db.IsMalicious(n.signer.NodeID())
		require.NoError(t, err)
		require.True(t, malicious)

		require.ErrorContains(t,
			n.hare.Handler(context.Background(), "", codec.MustEncode(msg2)),
			"dropped by graded",
		)
	})
}

func gatx(id types.ATXID, epoch types.EpochID, smesher types.NodeID, base, height uint64) types.VerifiedActivationTx {
	atx := &types.ActivationTx{}
	atx.NumUnits = 10
	atx.PublishEpoch = epoch
	atx.SmesherID = smesher
	atx.SetID(id)
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Time{}.Add(1))
	nonce := types.VRFPostIndex(1)
	atx.VRFNonce = &nonce
	verified, err := atx.Verify(base, height-base)
	if err != nil {
		panic(err)
	}
	return *verified
}

func gproposal(id types.ProposalID, atxid types.ATXID, smesher types.NodeID, layer types.LayerID, beacon types.Beacon) types.Proposal {
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
	return proposal
}

func gref(id types.ProposalID, atxid types.ATXID, smesher types.NodeID, layer types.LayerID, ref types.ProposalID) types.Proposal {
	proposal := types.Proposal{}
	proposal.Layer = layer
	proposal.RefBallot = types.BallotID(ref)
	proposal.AtxID = atxid
	proposal.SmesherID = smesher
	proposal.Ballot.SmesherID = smesher
	proposal.SetID(id)
	proposal.Ballot.SetID(types.BallotID(id))
	return proposal
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
	publish := types.EpochID(1)
	layer := (publish + 1).FirstLayer()
	goodBeacon := types.Beacon{1}
	badBeacon := types.Beacon{2}

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	for _, tc := range []struct {
		desc      string
		atxs      []types.VerifiedActivationTx
		proposals []types.Proposal
		malicious []types.NodeID
		layer     types.LayerID
		beacon    types.Beacon
		expect    []types.ProposalID
	}{
		{
			desc:   "sanity",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.VerifiedActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
			},
			proposals: []types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gproposal(pids[1], atxids[1], ids[1], layer, goodBeacon),
			},
			expect: []types.ProposalID{pids[0], pids[1]},
		},
		{
			desc:   "reference",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.VerifiedActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
			},
			proposals: []types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gproposal(pids[1], atxids[1], ids[1], layer.Sub(1), goodBeacon),
				gref(pids[2], atxids[1], ids[1], layer, pids[1]),
			},
			expect: []types.ProposalID{pids[0], pids[2]},
		},
		{
			desc:   "no reference",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.VerifiedActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
			},
			proposals: []types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gref(pids[2], atxids[1], ids[1], layer, pids[1]),
			},
			expect: []types.ProposalID{},
		},
		{
			desc:   "reference to reference",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.VerifiedActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
			},
			proposals: []types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gref(pids[1], atxids[1], ids[1], layer.Sub(1), pids[1]),
				gref(pids[2], atxids[1], ids[1], layer, pids[1]),
			},
			expect: []types.ProposalID{},
		},
		{
			desc:   "empty reference",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.VerifiedActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
			},
			proposals: []types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gref(pids[1], atxids[1], ids[1], layer, types.EmptyProposalID),
			},
			expect: []types.ProposalID{},
		},
		{
			desc:   "mismatched beacon",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.VerifiedActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
			},
			proposals: []types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gproposal(pids[1], atxids[1], ids[1], layer, badBeacon),
			},
			expect: []types.ProposalID{pids[0]},
		},
		{
			desc:   "multiproposals",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.VerifiedActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
			},
			proposals: []types.Proposal{
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
			atxs: []types.VerifiedActivationTx{
				gatx(atxids[0], publish, ids[0], 101, 1000),
				gatx(atxids[1], publish, signer.NodeID(), 10, 100),
			},
			proposals: []types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gproposal(pids[1], atxids[1], ids[1], layer, goodBeacon),
			},
			expect: []types.ProposalID{pids[1]},
		},
		{
			desc:   "malicious",
			layer:  layer,
			beacon: goodBeacon,
			atxs: []types.VerifiedActivationTx{
				gatx(atxids[0], publish, ids[0], 10, 100),
				gatx(atxids[1], publish, ids[1], 10, 100),
			},
			proposals: []types.Proposal{
				gproposal(pids[0], atxids[0], ids[0], layer, goodBeacon),
				gproposal(pids[1], atxids[1], ids[1], layer, goodBeacon),
			},
			malicious: []types.NodeID{ids[0]},
			expect:    []types.ProposalID{pids[1]},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			db := datastore.NewCachedDB(sql.InMemory(), log.NewNop())
			hare := New(nil, nil, db, nil, signer, nil, nil, WithLogger(logtest.New(t).Zap()))
			for _, atx := range tc.atxs {
				require.NoError(t, atxs.Add(db, &atx))
			}
			for _, proposal := range tc.proposals {
				require.NoError(t, proposals.Add(db, &proposal))
				require.NoError(t, ballots.Add(db, &proposal.Ballot))
			}
			for _, id := range tc.malicious {
				require.NoError(t, identities.SetMalicious(db, id, []byte("non empty"), time.Time{}))
			}
			require.Equal(t, tc.expect, hare.Proposals(tc.layer, tc.beacon))
		})
	}
}
