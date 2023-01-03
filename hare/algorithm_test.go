package hare

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
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

var cfg = config.Config{N: 10, F: 5, RoundDuration: 2, ExpectedLeaders: 5, LimitIterations: 1000, LimitConcurrent: 1000}

func newRoundClockFromCfg(logger log.Log, cfg config.Config) *SimpleRoundClock {
	return NewSimpleRoundClock(time.Now(),
		time.Duration(cfg.WakeupDelta)*time.Second,
		time.Duration(cfg.RoundDuration)*time.Second,
	)
}

type mockMessageValidator struct {
	syntaxValid  bool
	contextValid error
	countSyntax  int
	countContext int
}

func (mmv *mockMessageValidator) SyntacticallyValidateMessage(context.Context, *Msg) bool {
	mmv.countSyntax++
	return mmv.syntaxValid
}

func (mmv *mockMessageValidator) ContextuallyValidateMessage(context.Context, *Msg, uint32) error {
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

func (mpt *mockProposalTracker) OnProposal(context.Context, *Msg) {
	mpt.countOnProposal++
}

func (mpt *mockProposalTracker) OnLateProposal(context.Context, *Msg) {
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

func (mct *mockCommitTracker) CommitCount() int {
	return 0
}

func (mct *mockCommitTracker) OnCommit(*Msg) {
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

func buildMessage(msg Message) *Msg {
	return &Msg{Message: msg, PubKey: nil}
}

type testBroker struct {
	*Broker
	mockStateQ *mocks.MockstateQuerier
	mockSyncS  *smocks.MockSyncStateProvider
}

func buildBroker(tb testing.TB, testName string) *testBroker {
	return buildBrokerWithLimit(tb, testName, cfg.LimitIterations)
}

func buildBrokerWithLimit(tb testing.TB, testName string, limit int) *testBroker {
	ctrl := gomock.NewController(tb)
	mockStateQ := mocks.NewMockstateQuerier(ctrl)
	mockSyncS := smocks.NewMockSyncStateProvider(ctrl)
	return &testBroker{
		Broker: newBroker(&mockEligibilityValidator{valid: 1}, mockStateQ, mockSyncS,
			4, limit, logtest.New(tb).WithName(testName)),
		mockSyncS:  mockSyncS,
		mockStateQ: mockStateQ,
	}
}

type mockEligibilityValidator struct {
	valid        int32
	validationFn func(context.Context, *Msg) bool
}

func (mev *mockEligibilityValidator) Validate(ctx context.Context, msg *Msg) bool {
	if mev.validationFn != nil {
		return mev.validationFn(ctx, msg)
	}
	return atomic.LoadInt32(&mev.valid) != 0
}

// test that a InnerMsg to a specific set ObjectID is delivered by the broker.
func TestConsensusProcess_Start(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	require.NoError(t, broker.Start(context.Background()))
	proc := generateConsensusProcess(t)
	inbox, err := broker.Register(context.Background(), proc.ID())
	require.NoError(t, err)
	proc.SetInbox(inbox)
	proc.value = NewSetFromValues(value1)
	err = proc.Start()
	require.NoError(t, err)
	err = proc.Start()
	require.Error(t, err, "instance already started")

	closeBrokerAndWait(t, broker.Broker)
}

func TestConsensusProcess_TerminationLimit(t *testing.T) {
	c := cfg
	c.LimitConcurrent = 1
	c.RoundDuration = 1
	p := generateConsensusProcessWithConfig(t, c)
	p.SetInbox(make(chan *Msg, 10))
	p.cfg.LimitIterations = 1
	p.cfg.RoundDuration = 1
	require.NoError(t, p.Start())
	time.Sleep(time.Duration(6*p.cfg.RoundDuration) * time.Second)

	assert.EqualValues(t, 1, p.getRound()/4)
}

func TestConsensusProcess_eventLoop(t *testing.T) {
	ctrl := gomock.NewController(t)

	net := &mockP2p{}
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	require.NoError(t, broker.Start(context.Background()))
	c := cfg
	c.F = 2
	proc := generateConsensusProcessWithConfig(t, c)
	proc.publisher = net

	mo := mocks.NewMockRolacle(ctrl)
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().Proof(gomock.Any(), proc.layer, proc.getRound()).Return(nil, nil).Times(2)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, gomock.Any()).Return(uint16(1), nil).Times(1)
	proc.oracle = mo

	proc.inbox, _ = broker.Register(context.Background(), proc.ID())
	proc.value = NewSetFromValues(value1, value2)
	proc.cfg.F = 2
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		proc.eventLoop()
		wg.Done()
	}()
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 1, net.getCount())
	proc.terminate()
	wg.Wait()
}

func TestConsensusProcess_handleMessage(t *testing.T) {
	ctrl := gomock.NewController(t)

	r := require.New(t)
	net := &mockP2p{}
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	r.NoError(broker.Start(context.Background()))
	proc := generateConsensusProcess(t)
	proc.publisher = net
	mo := mocks.NewMockRolacle(ctrl)
	proc.oracle = mo
	mValidator := &mockMessageValidator{}
	proc.validator = mValidator
	proc.inbox, _ = broker.Register(context.Background(), proc.ID())
	signer, err := signing.NewEdSigner()
	r.NoError(err)
	msg := BuildPreRoundMsg(signer, NewSetFromValues(value1), []byte{1})
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
	require.NoError(t, broker.Start(context.Background()))
	proc := generateConsensusProcess(t)
	proc.inbox, _ = broker.Register(context.Background(), proc.ID())
	proc.advanceToNextRound(context.Background())
	proc.advanceToNextRound(context.Background())
	assert.EqualValues(t, 1, proc.getRound())
	proc.advanceToNextRound(context.Background())
	assert.EqualValues(t, 2, proc.getRound())
}

func generateConsensusProcess(t *testing.T) *consensusProcess {
	return generateConsensusProcessWithConfig(t, cfg)
}

func generateConsensusProcessWithConfig(tb testing.TB, cfg config.Config) *consensusProcess {
	tb.Helper()
	logger := logtest.New(tb)
	s := NewSetFromValues(value1)
	oracle := eligibility.New(logger)
	edSigner, err := signing.NewEdSigner()
	require.NoError(tb, err)
	edPubkey := edSigner.PublicKey()
	nid := types.BytesToNodeID(edPubkey.Bytes())
	oracle.Register(true, nid)
	output := make(chan TerminationOutput, 1)

	ctrl := gomock.NewController(tb)
	sq := mocks.NewMockstateQuerier(ctrl)
	sq.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	return newConsensusProcess(context.Background(), cfg, instanceID1, s, oracle, sq,
		4, edSigner, types.BytesToNodeID(edPubkey.Bytes()),
		noopPubSub(tb), output, truer{}, newRoundClockFromCfg(logger, cfg),
		logtest.New(tb).WithName(edPubkey.String()))
}

func TestConsensusProcess_Id(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.layer = instanceID1
	assert.Equal(t, instanceID1, proc.ID())
}

func TestNewConsensusProcess_AdvanceToNextRound(t *testing.T) {
	proc := generateConsensusProcess(t)
	k := proc.getRound()
	proc.advanceToNextRound(context.Background())
	assert.Equal(t, k+1, proc.getRound())
}

func TestConsensusProcess_CreateInbox(t *testing.T) {
	proc := generateConsensusProcess(t)
	inbox := make(chan *Msg, 100)
	proc.SetInbox(inbox)
	assert.NotNil(t, proc.inbox)
	assert.Equal(t, 100, cap(proc.inbox))
}

func TestConsensusProcess_InitDefaultBuilder(t *testing.T) {
	proc := generateConsensusProcess(t)
	s := NewEmptySet(defaultSetSize)
	s.Add(value1)
	builder, err := proc.initDefaultBuilder(s)
	assert.Nil(t, err)
	assert.True(t, NewSet(builder.inner.Values).Equals(s))
	verifier := builder.msg.PubKey
	assert.Nil(t, verifier)
	assert.Equal(t, builder.inner.Round, proc.getRound())
	assert.Equal(t, builder.inner.CommittedRound, proc.committedRound)
	assert.Equal(t, builder.inner.Layer, proc.layer)
}

func TestConsensusProcess_isEligible_NotEligible(t *testing.T) {
	ctrl := gomock.NewController(t)

	proc := generateConsensusProcess(t)
	mo := mocks.NewMockRolacle(ctrl)
	proc.oracle = mo

	mo.EXPECT().Proof(gomock.Any(), proc.layer, proc.getRound()).Return(nil, nil).Times(1)
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, gomock.Any()).Return(uint16(0), nil).Times(1)
	assert.False(t, proc.shouldParticipate(context.Background()))
}

func TestConsensusProcess_isEligible_Eligible(t *testing.T) {
	ctrl := gomock.NewController(t)

	proc := generateConsensusProcess(t)
	mo := mocks.NewMockRolacle(ctrl)
	proc.oracle = mo

	mo.EXPECT().Proof(gomock.Any(), proc.layer, proc.getRound()).Return(nil, nil).Times(1)
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, gomock.Any()).Return(uint16(1), nil).Times(1)
	assert.True(t, proc.shouldParticipate(context.Background()))
}

func TestConsensusProcess_isEligible_ActiveSetFailed(t *testing.T) {
	ctrl := gomock.NewController(t)

	proc := generateConsensusProcess(t)
	mo := mocks.NewMockRolacle(ctrl)
	proc.oracle = mo

	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(false, errors.New("some err")).Times(1)
	assert.False(t, proc.shouldParticipate(context.Background()))
}

func TestConsensusProcess_isEligible_NotActive(t *testing.T) {
	ctrl := gomock.NewController(t)

	proc := generateConsensusProcess(t)
	mo := mocks.NewMockRolacle(ctrl)
	proc.oracle = mo

	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(false, nil).Times(1)
	assert.False(t, proc.shouldParticipate(context.Background()))
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
	m := BuildPreRoundMsg(signer, s, []byte{1})
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
	proc.validator.(*syntaxContextValidator).statusValidator = func(m *Msg) bool {
		return true
	}
	proc.SetInbox(make(chan *Msg))
	proc.advanceToNextRound(context.Background())
	proc.advanceToNextRound(context.Background())
	s := NewSetFromValues(value1)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildProposalMsg(signer, s)
	mpt := &mockProposalTracker{}
	proc.proposalTracker = mpt
	proc.handleMessage(context.Background(), m)
	// proc.processProposalMsg(m)
	assert.Equal(t, 1, mpt.countOnProposal)
	proc.advanceToNextRound(context.Background())
	proc.handleMessage(context.Background(), m)
	assert.Equal(t, 1, mpt.countOnLateProposal)
}

func TestConsensusProcess_procCommit(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound(context.Background())
	s := NewDefaultEmptySet()
	proc.commitTracker = newCommitTracker(1, 1, s)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildCommitMsg(signer, s)
	mct := &mockCommitTracker{}
	proc.commitTracker = mct
	proc.processCommitMsg(context.Background(), m)
	assert.Equal(t, 1, mct.countOnCommit)
}

func TestConsensusProcess_procNotify(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.notifyTracker = newNotifyTracker(proc.cfg.N)
	proc.advanceToNextRound(context.Background())
	s := NewSetFromValues(value1)
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildNotifyMsg(signer1, s)
	proc.processNotifyMsg(context.Background(), m)
	assert.Equal(t, 1, len(proc.notifyTracker.notifies))
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	m = BuildNotifyMsg(signer2, s)
	proc.committedRound = 0
	m.InnerMsg.Round = proc.committedRound
	proc.value.Add(value5)
	proc.setRound(notifyRound)
	proc.processNotifyMsg(context.Background(), m)
	assert.True(t, s.Equals(proc.value))
}

func TestConsensusProcess_Termination(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.notifyTracker = newNotifyTracker(proc.cfg.N)
	proc.advanceToNextRound(context.Background())
	s := NewSetFromValues(value1)

	for i := 0; i < cfg.F+1; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		proc.processNotifyMsg(context.Background(), BuildNotifyMsg(signer, s))
	}

	require.Equal(t, true, proc.terminating())
}

func TestConsensusProcess_currentRound(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound(context.Background())
	assert.Equal(t, statusRound, proc.currentRound())
	proc.advanceToNextRound(context.Background())
	assert.Equal(t, proposalRound, proc.currentRound())
	proc.advanceToNextRound(context.Background())
	assert.Equal(t, commitRound, proc.currentRound())
	proc.advanceToNextRound(context.Background())
	assert.Equal(t, notifyRound, proc.currentRound())
}

func TestConsensusProcess_onEarlyMessage(t *testing.T) {
	r := require.New(t)
	proc := generateConsensusProcess(t)
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer1, NewDefaultEmptySet(), nil)
	proc.advanceToNextRound(context.Background())
	proc.onEarlyMessage(context.Background(), buildMessage(Message{}))
	r.Len(proc.pending, 0)
	proc.onEarlyMessage(context.Background(), m)
	r.Len(proc.pending, 1)
	proc.onEarlyMessage(context.Background(), m)
	r.Len(proc.pending, 1)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	m2 := BuildPreRoundMsg(signer2, NewDefaultEmptySet(), nil)
	proc.onEarlyMessage(context.Background(), m2)
	signer3, err := signing.NewEdSigner()
	require.NoError(t, err)
	m3 := BuildPreRoundMsg(signer3, NewDefaultEmptySet(), nil)
	proc.onEarlyMessage(context.Background(), m3)
	r.Len(proc.pending, 3)
	proc.onRoundBegin(context.Background())

	// make sure we wait enough for the go routine to be executed
	r.Eventually(func() bool { return len(proc.pending) == 0 },
		time.Second, 100*time.Millisecond, "expected proc.pending to have zero length")
}

func TestProcOutput_Id(t *testing.T) {
	po := procReport{instanceID1, nil, false, false}
	assert.Equal(t, po.ID(), instanceID1)
}

func TestProcOutput_Set(t *testing.T) {
	es := NewDefaultEmptySet()
	po := procReport{instanceID1, es, false, false}
	assert.True(t, es.Equals(po.Set()))
}

func TestIterationFromCounter(t *testing.T) {
	for i := uint32(0); i < 10; i++ {
		assert.Equal(t, i/4, iterationFromCounter(i))
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
	mo.EXPECT().Proof(gomock.Any(), proc.layer, proc.getRound()).Return(nil, nil).Times(2)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, gomock.Any()).Return(uint16(1), nil).Times(1)
	proc.oracle = mo

	s := NewDefaultEmptySet()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, s, nil)
	proc.statusesTracker.RecordStatus(context.Background(), m)

	preStatusTracker := proc.statusesTracker
	proc.beginStatusRound(context.Background())
	assert.Equal(t, 1, network.getCount())
	assert.NotEqual(t, preStatusTracker, proc.statusesTracker)
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
	mo.EXPECT().Proof(gomock.Any(), proc.layer, proc.getRound()).Return(nil, nil).Times(2)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, gomock.Any()).Return(uint16(1), nil).Times(1)
	proc.oracle = mo

	statusTracker := newStatusTracker(1, 1)
	s := NewSetFromValues(value1)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	statusTracker.RecordStatus(context.Background(), BuildStatusMsg(signer, s))
	statusTracker.AnalyzeStatuses(validate)
	proc.statusesTracker = statusTracker

	proc.SetInbox(make(chan *Msg, 1))
	proc.beginProposalRound(context.Background())

	assert.Equal(t, 1, network.getCount())
	assert.Nil(t, proc.statusesTracker)
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
	mpt.proposedSet = NewSetFromValues(value1)

	preCommitTracker := proc.commitTracker
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().Proof(gomock.Any(), proc.layer, proc.getRound()).Return(nil, nil).Times(1)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, gomock.Any()).Return(uint16(0), nil).Times(1)
	proc.beginCommitRound(context.Background())
	assert.NotEqual(t, preCommitTracker, proc.commitTracker)

	mpt.isConflicting = false
	mpt.proposedSet = NewSetFromValues(value1)
	proc.SetInbox(make(chan *Msg, 1))
	mo.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), proc.layer).Return(true, nil).Times(1)
	mo.EXPECT().Proof(gomock.Any(), proc.layer, proc.getRound()).Return(nil, nil).Times(2)
	mo.EXPECT().CalcEligibility(gomock.Any(), proc.layer, proc.getRound(), gomock.Any(), proc.nid, gomock.Any()).Return(uint16(1), nil).Times(1)
	proc.beginCommitRound(context.Background())
	assert.Equal(t, 1, network.getCount())
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
	proc.SetInbox(make(chan *Msg, 100))
	const count = 5
	pending := make(map[string]*Msg)
	for i := 0; i < count; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		m := BuildStatusMsg(signer, NewSetFromValues(value1))
		pending[signer.PublicKey().String()] = m
	}
	proc.handlePending(pending)
	assert.Equal(t, count, len(proc.inbox))
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
	mpt.proposedSet = NewSetFromValues(value1)
	proc.beginNotifyRound(context.Background())
	r.True(proc.value.Equals(mpt.proposedSet))
	r.Equal(mct.certificate, proc.certificate)
	r.Nil(proc.proposalTracker)
	r.Nil(proc.commitTracker)
	r.Equal(1, net.callBroadcast)
}
