package hare

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

func newRoundClockFromCfg(logger log.Log, cfg config.Config) *SimpleRoundClock {
	return NewSimpleRoundClock(time.Now(), cfg.WakeupDelta, cfg.RoundDuration)
}

type mockMessageValidator struct {
	syntaxValid  bool
	contextValid error
	countSyntax  int
	countContext int
}

func (mmv *mockMessageValidator) SyntacticallyValidateMessage(context.Context, *Message) bool {
	mmv.countSyntax++
	return mmv.syntaxValid
}

func (mmv *mockMessageValidator) ContextuallyValidateMessage(context.Context, *Message, uint32) error {
	mmv.countContext++
	return mmv.contextValid
}

type mockP2p struct {
	mu    sync.RWMutex
	count int
	err   error
}

func (m *mockP2p) Publish(context.Context, string, []byte) error {
	m.incCount()
	return m.getErr()
}

func (m *mockP2p) getCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.count
}

func (m *mockP2p) incCount() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.count++
}

func (m *mockP2p) getErr() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.err
}

func (m *mockP2p) setErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.err = err
}

type mockProposalTracker struct {
	isConflicting       bool
	proposedSet         *Set
	countOnProposal     int
	countOnLateProposal int
	countIsConflicting  int
	countProposedSet    int
}

func (mpt *mockProposalTracker) OnProposal(context.Context, *Message) {
	mpt.countOnProposal++
}

func (mpt *mockProposalTracker) OnLateProposal(context.Context, *Message) {
	mpt.countOnLateProposal++
}

func (mpt *mockProposalTracker) IsConflicting() bool {
	mpt.countIsConflicting++
	return mpt.isConflicting
}

func (mpt *mockProposalTracker) ProposedSet() *Set {
	mpt.countProposedSet++
	return mpt.proposedSet
}

type mockCommitTracker struct {
	countOnCommit         int
	countHasEnoughCommits int
	countBuildCertificate int
	hasEnoughCommits      bool
	certificate           *Certificate
}

func (mct *mockCommitTracker) CommitCount() *CountInfo {
	return &CountInfo{}
}

func (mct *mockCommitTracker) OnCommit(context.Context, *Message) {
	mct.countOnCommit++
}

func (mct *mockCommitTracker) HasEnoughCommits() bool {
	mct.countHasEnoughCommits++
	return mct.hasEnoughCommits
}

func (mct *mockCommitTracker) BuildCertificate() *Certificate {
	mct.countBuildCertificate++
	return mct.certificate
}

type testBroker struct {
	*Broker
	mockMesh   *mocks.Mockmesh
	mockStateQ *mocks.MockstateQuerier
	mockSyncS  *smocks.MockSyncStateProvider
}

func buildBroker(tb testing.TB, testName string) *testBroker {
	return buildBrokerWithLimit(tb, testName, 1000)
}

func buildBrokerWithLimit(tb testing.TB, testName string, limit int) *testBroker {
	ctrl := gomock.NewController(tb)
	mockStateQ := mocks.NewMockstateQuerier(ctrl)
	mockSyncS := smocks.NewMockSyncStateProvider(ctrl)
	mockMesh := mocks.NewMockmesh(ctrl)
	mch := make(chan *types.MalfeasanceGossip, 1)
	edVerifier, err := signing.NewEdVerifier()
	require.NoError(tb, err)
	return &testBroker{
		Broker: newBroker(mockMesh, edVerifier, &mockEligibilityValidator{valid: 1}, mockStateQ, mockSyncS,
			mch, limit, logtest.New(tb).WithName(testName)),
		mockMesh:   mockMesh,
		mockSyncS:  mockSyncS,
		mockStateQ: mockStateQ,
	}
}

type mockEligibilityValidator struct {
	valid int32
}

func (mev *mockEligibilityValidator) Validate(ctx context.Context, msg *Message) bool {
	return atomic.LoadInt32(&mev.valid) != 0
}

func (mev *mockEligibilityValidator) ValidateEligibilityGossip(context.Context, *types.HareEligibilityGossip) bool {
	return atomic.LoadInt32(&mev.valid) != 0
}

func TestConsensusProcess_TerminationLimit(t *testing.T) {
	c := config.Config{N: 10, RoundDuration: 200 * time.Millisecond, ExpectedLeaders: 5, LimitIterations: 1, LimitConcurrent: 1, Hdist: 20}
	p := generateConsensusProcessWithConfig(t, c, make(chan any, 10))
	p.Start()

	require.Eventually(t, func() bool {
		return p.getRound()/4 == 1
	}, 2*time.Second, 200*time.Millisecond)
}

func TestConsensusProcess_PassiveParticipant(t *testing.T) {
	c := config.Config{N: 10, RoundDuration: 200 * time.Millisecond, ExpectedLeaders: 5, LimitIterations: 1, LimitConcurrent: 1, Hdist: 20}
	p := generateConsensusProcessWithConfig(t, c, make(chan any, 10))
	p.nonce = nil
	p.Start()
	require.Eventually(t, func() bool {
		return p.getRound()/4 == uint32(1)
	}, 2*time.Second, 200*time.Millisecond)
}

func TestConsensusProcess_eventLoop(t *testing.T) {
	net := &mockP2p{}
	c := config.Config{N: 10, RoundDuration: 2 * time.Second, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 1000, Hdist: 20}
	proc := generateConsensusProcessWithConfig(t, c, make(chan any, 10))
	proc.publisher = net

	mo := mocks.NewMockRolacle(gomock.NewController(t))
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().Proof(gomock.Any(), gomock.Any(), proc.layer, proc.getRound()).Return(types.EmptyVrfSignature, nil).Times(2)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, *proc.nonce, gomock.Any()).Return(uint16(1), nil).Times(1)
	proc.oracle = mo
	proc.value = NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		proc.eventLoop()
		wg.Done()
	}()
	time.Sleep(500 * time.Millisecond)
	require.Equal(t, 1, net.getCount())
	proc.terminate()
	wg.Wait()
}

// test that proc.Stop() actually returns and cause the consensus process to be GC'ed.
func TestConsensusProcess_StartAndStop(t *testing.T) {
	c := config.Config{N: 10, RoundDuration: 50 * time.Millisecond, ExpectedLeaders: 5, LimitIterations: 1, LimitConcurrent: 1000, Hdist: 20}
	proc := generateConsensusProcessWithConfig(t, c, make(chan any, 10))
	proc.publisher = &mockP2p{}

	mo := mocks.NewMockRolacle(gomock.NewController(t))
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).AnyTimes()
	mo.EXPECT().Proof(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(types.EmptyVrfSignature, nil).AnyTimes()
	mo.EXPECT().CalcEligibility(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), proc.nid, *proc.nonce, gomock.Any()).Return(uint16(1), nil).AnyTimes()
	proc.oracle = mo

	proc.value = NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	proc.Start()
	require.Eventually(t, func() bool {
		proc.Stop()
		return true
	}, 500*time.Millisecond, 100*time.Millisecond)
}

func TestConsensusProcess_handleMessage(t *testing.T) {
	ctrl := gomock.NewController(t)

	r := require.New(t)
	net := &mockP2p{}
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.Start(context.Background())
	proc := generateConsensusProcess(t)
	proc.publisher = net
	mo := mocks.NewMockRolacle(ctrl)
	proc.oracle = mo
	mValidator := &mockMessageValidator{}
	proc.validator = mValidator
	signer, err := signing.NewEdSigner()
	r.NoError(err)
	msg := BuildPreRoundMsg(signer, NewSetFromValues(types.ProposalID{1}), types.VrfSignature{3})
	mValidator.syntaxValid = false
	r.False(proc.preRoundTracker.coinflip, "default coinflip should be false")
	proc.handleMessage(context.Background(), msg)
	r.False(proc.preRoundTracker.coinflip, "invalid message should not change coinflip")
	r.Equal(1, mValidator.countSyntax)
	r.Equal(1, mValidator.countContext)
	mValidator.syntaxValid = true
	proc.handleMessage(context.Background(), msg)
	r.NotEqual(0, mValidator.countContext)
	mValidator.contextValid = nil
	proc.handleMessage(context.Background(), msg)
	r.True(proc.preRoundTracker.coinflip)
	r.Equal(0, len(proc.pending))
	r.Equal(3, mValidator.countContext)
	r.Equal(3, mValidator.countSyntax)
	mValidator.contextValid = errors.New("not valid")
	proc.handleMessage(context.Background(), msg)
	r.True(proc.preRoundTracker.coinflip)
	r.Equal(4, mValidator.countContext)
	r.Equal(3, mValidator.countSyntax)
	r.Equal(0, len(proc.pending))
	mValidator.contextValid = errEarlyMsg
	proc.handleMessage(context.Background(), msg)
	r.True(proc.preRoundTracker.coinflip)
	r.Equal(1, len(proc.pending))
}

func TestConsensusProcess_nextRound(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.Start(context.Background())
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound(context.Background())
	proc.advanceToNextRound(context.Background())
	require.EqualValues(t, 1, proc.getRound())
	proc.advanceToNextRound(context.Background())
	require.EqualValues(t, 2, proc.getRound())
}

func generateConsensusProcess(t *testing.T) *consensusProcess {
	cfg := config.Config{N: 10, RoundDuration: 2 * time.Second, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 1000, Hdist: 20}
	return generateConsensusProcessWithConfig(t, cfg, make(chan any, 1000))
}

func generateConsensusProcessWithConfig(tb testing.TB, cfg config.Config, inbox chan any) *consensusProcess {
	tb.Helper()
	logger := logtest.New(tb)
	oracle := eligibility.New(logger)
	edSigner, err := signing.NewEdSigner()
	require.NoError(tb, err)
	edVerifier, err := signing.NewEdVerifier()
	require.NoError(tb, err)
	edPubkey := edSigner.PublicKey()
	nid := types.BytesToNodeID(edPubkey.Bytes())
	oracle.Register(true, nid)
	output := make(chan report, 1)
	wc := make(chan wcReport, 1)
	nonce := types.VRFPostIndex(rand.Uint64())

	sq := mocks.NewMockstateQuerier(gomock.NewController(tb))
	sq.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	comm := communication{
		inbox:  inbox,
		mchOut: make(chan *types.MalfeasanceGossip),
		report: output,
		wc:     wc,
	}
	return newConsensusProcess(
		context.Background(),
		cfg,
		instanceID1,
		NewSetFromValues(types.ProposalID{1}),
		oracle,
		sq,
		edSigner,
		edVerifier,
		types.BytesToNodeID(edPubkey.Bytes()),
		&nonce,
		noopPubSub(tb),
		comm,
		truer{},
		newRoundClockFromCfg(logger, cfg),
		logger.WithName(edPubkey.String()),
	)
}

func TestConsensusProcess_Id(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.layer = instanceID1
	require.Equal(t, instanceID1, proc.ID())
}

func TestNewConsensusProcess_AdvanceToNextRound(t *testing.T) {
	proc := generateConsensusProcess(t)
	k := proc.getRound()
	proc.advanceToNextRound(context.Background())
	require.Equal(t, k+1, proc.getRound())
}

func TestConsensusProcess_InitDefaultBuilder(t *testing.T) {
	proc := generateConsensusProcess(t)
	s := NewEmptySet(defaultSetSize)
	s.Add(types.ProposalID{1})
	builder, err := proc.initDefaultBuilder(s)
	require.Nil(t, err)
	require.True(t, NewSet(builder.inner.Values).Equals(s))
	require.Equal(t, builder.msg.Round, proc.getRound())
	require.Equal(t, builder.inner.CommittedRound, proc.committedRound)
	require.Equal(t, builder.msg.Layer, proc.layer)
}

func TestConsensusProcess_isEligible_NotEligible(t *testing.T) {
	ctrl := gomock.NewController(t)

	proc := generateConsensusProcess(t)
	mo := mocks.NewMockRolacle(ctrl)
	proc.oracle = mo

	mo.EXPECT().Proof(gomock.Any(), *proc.nonce, proc.layer, proc.getRound()).Return(types.EmptyVrfSignature, nil).Times(1)
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, *proc.nonce, gomock.Any()).Return(uint16(0), nil).Times(1)
	require.False(t, proc.shouldParticipate(context.Background()))
}

func TestConsensusProcess_isEligible_Eligible(t *testing.T) {
	ctrl := gomock.NewController(t)

	proc := generateConsensusProcess(t)
	mo := mocks.NewMockRolacle(ctrl)
	proc.oracle = mo

	mo.EXPECT().Proof(gomock.Any(), *proc.nonce, proc.layer, proc.getRound()).Return(types.EmptyVrfSignature, nil).Times(1)
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, *proc.nonce, gomock.Any()).Return(uint16(1), nil).Times(1)
	require.True(t, proc.shouldParticipate(context.Background()))
}

func TestConsensusProcess_isEligible_ActiveSetFailed(t *testing.T) {
	ctrl := gomock.NewController(t)

	proc := generateConsensusProcess(t)
	mo := mocks.NewMockRolacle(ctrl)
	proc.oracle = mo

	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(false, errors.New("some err")).Times(1)
	require.False(t, proc.shouldParticipate(context.Background()))
}

func TestConsensusProcess_isEligible_NotActive(t *testing.T) {
	ctrl := gomock.NewController(t)

	proc := generateConsensusProcess(t)
	mo := mocks.NewMockRolacle(ctrl)
	proc.oracle = mo

	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(false, nil).Times(1)
	require.False(t, proc.shouldParticipate(context.Background()))
}

func TestConsensusProcess_sendMessage(t *testing.T) {
	r := require.New(t)
	net := &mockP2p{}

	proc := generateConsensusProcess(t)
	proc.publisher = net

	b := proc.sendMessage(context.Background(), nil)
	r.Equal(0, net.getCount())
	r.False(b)
	signer, err := signing.NewEdSigner()
	r.NoError(err)
	msg := buildStatusMsg(signer, proc.value, 0)

	net.setErr(errors.New("mock network failed error"))
	b = proc.sendMessage(context.Background(), msg)
	r.False(b)
	r.Equal(1, net.getCount())

	net.setErr(nil)
	b = proc.sendMessage(context.Background(), msg)
	r.True(b)
}

func TestConsensusProcess_procPre(t *testing.T) {
	proc := generateConsensusProcess(t)
	s := NewDefaultEmptySet()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, s, types.VrfSignature{3})
	require.False(t, proc.preRoundTracker.coinflip)
	proc.processPreRoundMsg(context.Background(), m)
	require.Len(t, proc.preRoundTracker.preRound, 1)
	require.True(t, proc.preRoundTracker.coinflip)
}

func TestConsensusProcess_procStatus(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.beginStatusRound(context.Background())
	s := NewDefaultEmptySet()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildStatusMsg(signer, s)
	proc.processStatusMsg(context.Background(), m)
	require.Len(t, proc.statusesTracker.statuses, 1)
}

func TestConsensusProcess_procProposal(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.validator.(*syntaxContextValidator).threshold = 1
	proc.validator.(*syntaxContextValidator).statusValidator = func(m *Message) bool {
		return true
	}
	proc.advanceToNextRound(context.Background())
	proc.advanceToNextRound(context.Background())
	s := NewSetFromValues(types.ProposalID{1})
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildProposalMsg(signer, s)
	mpt := &mockProposalTracker{}
	proc.proposalTracker = mpt
	proc.handleMessage(context.Background(), m)
	// proc.processProposalMsg(m)
	require.Equal(t, 1, mpt.countOnProposal)
	proc.advanceToNextRound(context.Background())
	proc.handleMessage(context.Background(), m)
	require.Equal(t, 1, mpt.countOnLateProposal)
}

func TestConsensusProcess_procCommit(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound(context.Background())
	s := NewDefaultEmptySet()
	proc.commitTracker = newCommitTracker(logtest.New(t), 6, make(chan *types.MalfeasanceGossip), proc.eTracker, 1, 1, s)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildCommitMsg(signer, s)
	mct := &mockCommitTracker{}
	proc.commitTracker = mct
	proc.processCommitMsg(context.Background(), m)
	require.Equal(t, 1, mct.countOnCommit)
}

func TestConsensusProcess_procNotify(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.notifyTracker = newNotifyTracker(logtest.New(t), 7, make(chan *types.MalfeasanceGossip), proc.eTracker, proc.cfg.N)
	proc.advanceToNextRound(context.Background())
	s := NewSetFromValues(types.ProposalID{1})
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildNotifyMsg(signer1, s)
	proc.processNotifyMsg(context.Background(), m)
	require.Equal(t, 1, len(proc.notifyTracker.notifies))
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	m = BuildNotifyMsg(signer2, s)
	proc.committedRound = 0
	m.Round = proc.committedRound
	proc.value.Add(types.ProposalID{5})
	proc.setRound(notifyRound)
	proc.processNotifyMsg(context.Background(), m)
	require.True(t, s.Equals(proc.value))
}

func TestConsensusProcess_Termination(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.notifyTracker = newNotifyTracker(logtest.New(t), notifyRound, make(chan *types.MalfeasanceGossip), proc.eTracker, proc.cfg.N)
	proc.advanceToNextRound(context.Background())
	s := NewSetFromValues(types.ProposalID{1})

	for i := 0; i < proc.cfg.N/2+1; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		m := BuildNotifyMsg(signer, s)
		proc.eTracker.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
		proc.processNotifyMsg(context.Background(), m)
	}

	require.Equal(t, true, proc.terminating())
}

func TestConsensusProcess_currentRound(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound(context.Background())
	require.Equal(t, statusRound, proc.currentRound())
	proc.advanceToNextRound(context.Background())
	require.Equal(t, proposalRound, proc.currentRound())
	proc.advanceToNextRound(context.Background())
	require.Equal(t, commitRound, proc.currentRound())
	proc.advanceToNextRound(context.Background())
	require.Equal(t, notifyRound, proc.currentRound())
}

func TestConsensusProcess_onEarlyMessage(t *testing.T) {
	r := require.New(t)
	proc := generateConsensusProcess(t)
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer1, NewDefaultEmptySet(), types.EmptyVrfSignature)
	proc.advanceToNextRound(context.Background())
	proc.onEarlyMessage(context.Background(), &Message{})
	r.Len(proc.pending, 0)
	proc.onEarlyMessage(context.Background(), m)
	r.Len(proc.pending, 1)
	proc.onEarlyMessage(context.Background(), m)
	r.Len(proc.pending, 1)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	m2 := BuildPreRoundMsg(signer2, NewDefaultEmptySet(), types.EmptyVrfSignature)
	proc.onEarlyMessage(context.Background(), m2)
	signer3, err := signing.NewEdSigner()
	require.NoError(t, err)
	m3 := BuildPreRoundMsg(signer3, NewDefaultEmptySet(), types.EmptyVrfSignature)
	proc.onEarlyMessage(context.Background(), m3)
	r.Len(proc.pending, 3)
	proc.onRoundBegin(context.Background())

	// make sure we wait enough for the go routine to be executed
	r.Eventually(func() bool { return len(proc.pending) == 0 },
		time.Second, 100*time.Millisecond, "expected proc.pending to have zero length")
}

func TestIterationFromCounter(t *testing.T) {
	for i := uint32(0); i < 10; i++ {
		require.Equal(t, i/4, inferIteration(i))
	}
}

func TestConsensusProcess_beginStatusRound(t *testing.T) {
	ctrl := gomock.NewController(t)

	proc := generateConsensusProcess(t)
	proc.advanceToNextRound(context.Background())
	proc.onRoundBegin(context.Background())
	network := &mockP2p{}
	proc.publisher = network

	mo := mocks.NewMockRolacle(ctrl)
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().Proof(gomock.Any(), *proc.nonce, proc.layer, proc.getRound()).Return(types.EmptyVrfSignature, nil).Times(2)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, *proc.nonce, gomock.Any()).Return(uint16(1), nil).Times(1)
	proc.oracle = mo

	s := NewDefaultEmptySet()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, s, types.EmptyVrfSignature)
	proc.statusesTracker.RecordStatus(context.Background(), m)

	preStatusTracker := proc.statusesTracker
	proc.beginStatusRound(context.Background())
	require.Equal(t, 1, network.getCount())
	require.NotEqual(t, preStatusTracker, proc.statusesTracker)
}

func TestConsensusProcess_beginProposalRound(t *testing.T) {
	ctrl := gomock.NewController(t)

	proc := generateConsensusProcess(t)
	proc.advanceToNextRound(context.Background())
	network := &mockP2p{}
	proc.publisher = network
	proc.round = 1

	mo := mocks.NewMockRolacle(ctrl)
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().Proof(gomock.Any(), *proc.nonce, proc.layer, proc.getRound()).Return(types.EmptyVrfSignature, nil).Times(2)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, *proc.nonce, gomock.Any()).Return(uint16(1), nil).Times(1)
	proc.oracle = mo

	statusTracker := newStatusTracker(logtest.New(t), statusRound, make(chan *types.MalfeasanceGossip), proc.eTracker, 1, 1)
	s := NewSetFromValues(types.ProposalID{1})
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildStatusMsg(signer, s)
	proc.eTracker.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
	statusTracker.RecordStatus(context.Background(), m)
	statusTracker.AnalyzeStatusMessages(func(*Message) bool { return true })
	proc.statusesTracker = statusTracker

	proc.beginProposalRound(context.Background())

	require.Equal(t, 1, network.getCount())
	require.Nil(t, proc.statusesTracker)
}

func TestConsensusProcess_beginCommitRound(t *testing.T) {
	ctrl := gomock.NewController(t)

	proc := generateConsensusProcess(t)
	network := &mockP2p{}
	proc.publisher = network

	mo := mocks.NewMockRolacle(ctrl)
	proc.oracle = mo

	mpt := &mockProposalTracker{}
	proc.proposalTracker = mpt
	mpt.proposedSet = NewSetFromValues(types.ProposalID{1})

	preCommitTracker := proc.commitTracker
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().Proof(gomock.Any(), *proc.nonce, proc.layer, proc.getRound()).Return(types.EmptyVrfSignature, nil).Times(1)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, *proc.nonce, gomock.Any()).Return(uint16(0), nil).Times(1)
	proc.beginCommitRound(context.Background())
	require.NotEqual(t, preCommitTracker, proc.commitTracker)

	mpt.isConflicting = false
	mpt.proposedSet = NewSetFromValues(types.ProposalID{1})
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().Proof(gomock.Any(), *proc.nonce, proc.layer, proc.getRound()).Return(types.EmptyVrfSignature, nil).Times(2)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, *proc.nonce, gomock.Any()).Return(uint16(1), nil).Times(1)
	proc.beginCommitRound(context.Background())
	require.Equal(t, 1, network.getCount())
}

type mockNet struct {
	callBroadcast int
	err           error
}

func (m *mockNet) Publish(ctx context.Context, protocol string, payload []byte) error {
	m.callBroadcast++
	return m.err
}

func TestConsensusProcess_handlePending(t *testing.T) {
	proc := generateConsensusProcess(t)
	const count = 5
	pending := make(map[types.NodeID]*Message)
	for i := 0; i < count; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		m := BuildStatusMsg(signer, NewSetFromValues(types.ProposalID{1}))
		pending[signer.NodeID()] = m
	}
	proc.handlePending(pending)
	require.Equal(t, count, len(proc.comm.inbox))
}

func TestConsensusProcess_beginRound4(t *testing.T) {
	r := require.New(t)

	proc := generateConsensusProcess(t)
	net := &mockNet{}
	proc.publisher = net
	mpt := &mockProposalTracker{}
	mct := &mockCommitTracker{}
	proc.proposalTracker = mpt
	proc.commitTracker = mct
	proc.beginNotifyRound(context.Background())
	r.Equal(1, mpt.countIsConflicting)
	r.Nil(proc.proposalTracker)
	r.Nil(proc.commitTracker)

	proc.proposalTracker = mpt
	proc.commitTracker = mct
	mpt.isConflicting = true
	proc.beginNotifyRound(context.Background())
	r.Equal(2, mpt.countIsConflicting)
	r.Equal(1, mct.countHasEnoughCommits)
	r.Nil(proc.proposalTracker)
	r.Nil(proc.commitTracker)

	proc.proposalTracker = mpt
	proc.commitTracker = mct
	mpt.isConflicting = false
	mct.hasEnoughCommits = false
	proc.beginNotifyRound(context.Background())
	r.Equal(0, mct.countBuildCertificate)
	r.Nil(proc.proposalTracker)
	r.Nil(proc.commitTracker)

	proc.proposalTracker = mpt
	proc.commitTracker = mct
	mct.hasEnoughCommits = true
	mct.certificate = nil
	proc.beginNotifyRound(context.Background())
	r.Equal(1, mct.countBuildCertificate)
	r.Equal(0, mpt.countProposedSet)
	r.Nil(proc.proposalTracker)
	r.Nil(proc.commitTracker)

	proc.proposalTracker = mpt
	proc.commitTracker = mct
	mct.certificate = &Certificate{}
	mpt.proposedSet = nil
	proc.value = NewDefaultEmptySet()
	proc.beginNotifyRound(context.Background())
	r.NotNil(proc.value)
	r.Nil(proc.proposalTracker)
	r.Nil(proc.commitTracker)

	proc.proposalTracker = mpt
	proc.commitTracker = mct
	mpt.proposedSet = NewSetFromValues(types.ProposalID{1})
	proc.beginNotifyRound(context.Background())
	r.True(proc.value.Equals(mpt.proposedSet))
	r.Equal(mct.certificate, proc.certificate)
	r.Nil(proc.proposalTracker)
	r.Nil(proc.commitTracker)
	r.Equal(1, net.callBroadcast)
}
