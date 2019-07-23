package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

var cfg = config.Config{N: 10, F: 5, RoundDuration: 2, ExpectedLeaders: 5}

type mockMessageValidator struct {
	syntaxValid  bool
	contextValid bool
	err          error
	countSyntax  int
	countContext int
}

func (mmv *mockMessageValidator) SyntacticallyValidateMessage(m *Msg) bool {
	mmv.countSyntax++
	return mmv.syntaxValid
}

func (mmv *mockMessageValidator) ContextuallyValidateMessage(m *Msg, expectedK int32) (bool, error) {
	mmv.countContext++
	return mmv.contextValid, mmv.err
}

type mockRolacle struct {
	isEligible bool
	err        error
	MockStateQuerier
}

func (mr *mockRolacle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	return mr.isEligible, mr.err
}

func (mr *mockRolacle) Proof(id types.NodeId, layer types.LayerID, round int32) ([]byte, error) {
	return []byte{}, nil
}

func (mr *mockRolacle) Register(id string) {
}

func (mr *mockRolacle) Unregister(id string) {
}

type mockP2p struct {
	count int
}

func (m *mockP2p) RegisterGossipProtocol(protocol string) chan service.GossipMessage {
	return make(chan service.GossipMessage)
}

func (m *mockP2p) Broadcast(protocol string, payload []byte) error {
	m.count++
	return nil
}

type mockProposalTracker struct {
	isConflicting       bool
	proposedSet         *Set
	countOnProposal     int
	countOnLateProposal int
	countIsConflicting  int
	countProposedSet    int
}

func (mpt *mockProposalTracker) OnProposal(msg *Msg) {
	mpt.countOnProposal++
}

func (mpt *mockProposalTracker) OnLateProposal(msg *Msg) {
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

func (mct *mockCommitTracker) OnCommit(msg *Msg) {
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

type mockSyncer struct {
	isSync bool
}

func (s *mockSyncer) IsSynced() bool {
	return s.isSync
}

func generateSigning(t *testing.T) Signer {
	return signing.NewEdSigner()
}

func buildMessage(msg *Message) *Msg {
	return &Msg{msg, nil}
}

func buildBroker(net NetworkService, testName string) *Broker {
	return NewBroker(net, &mockEligibilityValidator{true}, MockStateQuerier{true, nil},
		(&mockSyncer{true}).IsSynced, 10, Closer{make(chan struct{})}, log.NewDefault(testName))
}

type mockEligibilityValidator struct {
	valid bool
}

func (mev *mockEligibilityValidator) Validate(m *Msg) bool {
	return mev.valid
}

type mockOracle struct {
}

func (mo *mockOracle) Eligible(instanceId InstanceId, k int32, pubKey string, proof []byte) bool {
	return true
}

func buildOracle(oracle Rolacle) Rolacle {
	return oracle
}

// test that a InnerMsg to a specific set objectId is delivered by the broker
func TestConsensusProcess_Start(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := buildBroker(n1, t.Name())
	broker.Start()
	proc := generateConsensusProcess(t)
	proc.s = NewSmallEmptySet()
	inbox, _ := broker.Register(proc.Id())
	proc.SetInbox(inbox)
	err := proc.Start()
	assert.Equal(t, "instance started with an empty set", err.Error())
	proc.s = NewSetFromValues(value1)
	err = proc.Start()
	assert.Equal(t, nil, err)
	err = proc.Start()
	assert.Equal(t, "instance already started", err.Error())
}

func TestConsensusProcess_eventLoop(t *testing.T) {
	net := &mockP2p{}
	broker := buildBroker(net, t.Name())
	broker.Start()
	proc := generateConsensusProcess(t)
	proc.network = net
	oracle := &mockRolacle{MockStateQuerier: MockStateQuerier{true, nil}}
	oracle.isEligible = true
	proc.oracle = oracle
	proc.inbox, _ = broker.Register(proc.Id())
	proc.s = NewSetFromValues(value1, value2)
	proc.cfg.F = 2
	go proc.eventLoop()
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 1, net.count)
}

func TestConsensusProcess_handleMessage(t *testing.T) {
	net := &mockP2p{}
	broker := buildBroker(net, t.Name())
	broker.Start()
	proc := generateConsensusProcess(t)
	proc.network = net
	oracle := &mockRolacle{}
	proc.oracle = oracle
	mValidator := &mockMessageValidator{}
	proc.validator = mValidator
	proc.inbox, _ = broker.Register(proc.Id())
	msg := BuildPreRoundMsg(generateSigning(t), NewSetFromValues(value1))
	oracle.isEligible = true
	mValidator.syntaxValid = false
	proc.handleMessage(msg)
	assert.Equal(t, 1, mValidator.countSyntax)
	mValidator.syntaxValid = true
	proc.handleMessage(msg)
	assert.NotEqual(t, 0, mValidator.countContext)
	mValidator.contextValid = true
	proc.handleMessage(msg)
	assert.Equal(t, 0, len(proc.pending))
	mValidator.contextValid = false
	proc.handleMessage(msg)
	assert.Equal(t, 0, len(proc.pending))
	mValidator.contextValid = false
	mValidator.err = errEarlyMsg
	proc.handleMessage(msg)
	assert.Equal(t, 1, len(proc.pending))
}

func TestConsensusProcess_nextRound(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := buildBroker(n1, t.Name())
	broker.Start()
	proc := generateConsensusProcess(t)
	proc.inbox, _ = broker.Register(proc.Id())
	proc.advanceToNextRound()
	proc.advanceToNextRound()
	assert.Equal(t, int32(1), proc.k)
	proc.advanceToNextRound()
	assert.Equal(t, int32(2), proc.k)
}

func generateConsensusProcess(t *testing.T) *ConsensusProcess {
	bn, _ := node.GenerateTestNode(t)
	sim := service.NewSimulator()
	n1 := sim.NewNodeFrom(bn.NodeInfo)

	s := NewSetFromValues(value1)
	oracle := eligibility.New()
	signing := signing.NewEdSigner()
	_, vrfPub := BLS381.GenKeyPair(BLS381.DefaultSeed())
	oracle.Register(true, signing.PublicKey().String())
	output := make(chan TerminationOutput, 1)

	return NewConsensusProcess(cfg, instanceId1, s, oracle, NewMockStateQuerier(), 10, signing, types.NodeId{Key: signing.PublicKey().String(), VRFPublicKey: vrfPub}, n1, output, log.NewDefault(signing.PublicKey().String()))
}

func TestConsensusProcess_Id(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.instanceId = instanceId1
	assert.Equal(t, instanceId1, proc.Id())
}

func TestNewConsensusProcess_AdvanceToNextRound(t *testing.T) {
	proc := generateConsensusProcess(t)
	k := proc.k
	proc.advanceToNextRound()
	assert.Equal(t, k+1, proc.k)
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
	assert.Equal(t, builder.inner.K, proc.k)
	assert.Equal(t, builder.inner.Ki, proc.ki)
	assert.Equal(t, InstanceId(builder.inner.InstanceId), proc.instanceId)
}

func TestConsensusProcess_isEligible(t *testing.T) {
	proc := generateConsensusProcess(t)
	oracle := &mockRolacle{MockStateQuerier: MockStateQuerier{true, nil}}
	proc.oracle = oracle
	oracle.isEligible = false
	assert.False(t, proc.shouldParticipate())
	oracle.isEligible = true
	assert.True(t, proc.shouldParticipate())
	oracle.MockStateQuerier = MockStateQuerier{false, errors.New("some err")}
	assert.False(t, proc.shouldParticipate())
	oracle.MockStateQuerier = MockStateQuerier{false, nil}
	assert.False(t, proc.shouldParticipate())
	oracle.MockStateQuerier = MockStateQuerier{true, nil}
	assert.True(t, proc.shouldParticipate())
}

func TestConsensusProcess_sendMessage(t *testing.T) {
	net := &mockP2p{}
	oracle := &mockRolacle{MockStateQuerier: MockStateQuerier{true, nil}}

	proc := generateConsensusProcess(t)
	proc.oracle = oracle
	proc.network = net

	proc.sendMessage(nil)
	assert.Equal(t, 0, net.count)
	msg := buildStatusMsg(generateSigning(t), proc.s, 0)

	oracle.isEligible = false
	proc.sendMessage(msg)
	assert.Equal(t, 0, net.count)

	oracle.isEligible = true
	proc.sendMessage(msg)
	assert.Equal(t, 1, net.count)
}

func TestConsensusProcess_procPre(t *testing.T) {
	proc := generateConsensusProcess(t)
	s := NewSmallEmptySet()
	m := BuildPreRoundMsg(generateSigning(t), s)
	proc.processPreRoundMsg(m)
	assert.Equal(t, 1, len(proc.preRoundTracker.preRound))
}

func TestConsensusProcess_procStatus(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.beginRound1()
	s := NewSmallEmptySet()
	m := BuildStatusMsg(generateSigning(t), s)
	proc.processStatusMsg(m)
	assert.Equal(t, 1, len(proc.statusesTracker.statuses))
}

func TestConsensusProcess_procProposal(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.validator.(*syntaxContextValidator).threshold = 1
	proc.validator.(*syntaxContextValidator).statusValidator = func(m *Msg) bool {
		return true
	}
	proc.SetInbox(make(chan *Msg))
	proc.advanceToNextRound()
	proc.advanceToNextRound()
	s := NewSetFromValues(value1)
	m := BuildProposalMsg(generateSigning(t), s)
	mpt := &mockProposalTracker{}
	proc.proposalTracker = mpt
	proc.handleMessage(m)
	//proc.processProposalMsg(m)
	assert.Equal(t, 1, mpt.countOnProposal)
	proc.advanceToNextRound()
	proc.handleMessage(m)
	assert.Equal(t, 1, mpt.countOnLateProposal)
}

func TestConsensusProcess_procCommit(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	s := NewSmallEmptySet()
	proc.commitTracker = NewCommitTracker(1, 1, s)
	m := BuildCommitMsg(generateSigning(t), s)
	mct := &mockCommitTracker{}
	proc.commitTracker = mct
	proc.processCommitMsg(m)
	assert.Equal(t, 1, mct.countOnCommit)
}

func TestConsensusProcess_procNotify(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	s := NewSetFromValues(value1)
	m := BuildNotifyMsg(generateSigning(t), s)
	proc.processNotifyMsg(m)
	assert.Equal(t, 1, len(proc.notifyTracker.notifies))
	m = BuildNotifyMsg(generateSigning(t), s)
	proc.ki = 0
	m.InnerMsg.K = proc.ki
	proc.s.Add(value5)
	proc.k = Round4
	proc.processNotifyMsg(m)
	assert.True(t, s.Equals(proc.s))
}

func TestConsensusProcess_Termination(t *testing.T) {

	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	s := NewSetFromValues(value1)

	for i := 0; i < cfg.F+1; i++ {
		proc.processNotifyMsg(BuildNotifyMsg(generateSigning(t), s))
	}

	timer := time.NewTimer(10 * time.Second)
	for {
		select {
		case <-timer.C:
			t.Fatal("Timeout")
		case <-proc.CloseChannel():
			return
		}
	}
}

func TestConsensusProcess_currentRound(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	assert.Equal(t, Round1, proc.currentRound())
	proc.advanceToNextRound()
	assert.Equal(t, Round2, proc.currentRound())
	proc.advanceToNextRound()
	assert.Equal(t, Round3, proc.currentRound())
	proc.advanceToNextRound()
	assert.Equal(t, Round4, proc.currentRound())
}

func TestConsensusProcess_onEarlyMessage(t *testing.T) {
	proc := generateConsensusProcess(t)
	m := BuildPreRoundMsg(generateSigning(t), NewSmallEmptySet())
	proc.advanceToNextRound()
	proc.onEarlyMessage(buildMessage(nil))
	assert.Equal(t, 0, len(proc.pending))
	proc.onEarlyMessage(m)
	assert.Equal(t, 1, len(proc.pending))
	proc.onEarlyMessage(m)
	assert.Equal(t, 1, len(proc.pending))
}

func TestProcOutput_Id(t *testing.T) {
	po := procOutput{instanceId1, nil}
	assert.Equal(t, po.Id(), instanceId1)
}

func TestProcOutput_Set(t *testing.T) {
	es := NewSmallEmptySet()
	po := procOutput{instanceId1, es}
	assert.True(t, es.Equals(po.Set()))
}

func TestIterationFromCounter(t *testing.T) {
	for i := 0; i < 10; i++ {
		assert.Equal(t, int32(i/4), iterationFromCounter(int32(i)))
	}
}

func TestConsensusProcess_beginRound1(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	proc.onRoundBegin()
	network := &mockP2p{}
	proc.network = network
	oracle := &mockRolacle{MockStateQuerier: MockStateQuerier{true, nil}}
	proc.oracle = oracle
	s := NewSmallEmptySet()
	m := BuildPreRoundMsg(generateSigning(t), s)
	proc.statusesTracker.RecordStatus(m)

	preStatusTracker := proc.statusesTracker
	oracle.isEligible = true
	proc.beginRound1()
	assert.Equal(t, 1, network.count)
	assert.NotEqual(t, preStatusTracker, proc.statusesTracker)
}

func TestConsensusProcess_beginRound2(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	network := &mockP2p{}
	proc.network = network
	oracle := &mockRolacle{MockStateQuerier: MockStateQuerier{true, nil}}
	proc.oracle = oracle
	oracle.isEligible = true

	statusTracker := NewStatusTracker(1, 1)
	s := NewSetFromValues(value1)
	statusTracker.RecordStatus(BuildStatusMsg(generateSigning(t), s))
	statusTracker.analyzed = true
	proc.statusesTracker = statusTracker

	proc.k = 1
	proc.SetInbox(make(chan *Msg, 1))
	proc.beginRound2()

	assert.Equal(t, 1, network.count)
	assert.Nil(t, proc.statusesTracker)
}

func TestConsensusProcess_beginRound3(t *testing.T) {
	proc := generateConsensusProcess(t)
	network := &mockP2p{}
	proc.network = network
	oracle := &mockRolacle{MockStateQuerier: MockStateQuerier{true, nil}}
	proc.oracle = oracle
	mpt := &mockProposalTracker{}
	proc.proposalTracker = mpt
	mpt.proposedSet = NewSetFromValues(value1)

	preCommitTracker := proc.commitTracker
	proc.beginRound3()
	assert.NotEqual(t, preCommitTracker, proc.commitTracker)

	mpt.isConflicting = false
	mpt.proposedSet = NewSetFromValues(value1)
	oracle.isEligible = true
	proc.SetInbox(make(chan *Msg, 1))
	proc.beginRound3()
	assert.Equal(t, 1, network.count)
}

func TestConsensusProcess_beginRound4(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.beginRound1()
	proc.beginRound2()
	proc.beginRound3()
	assert.NotNil(t, proc.commitTracker)
	assert.NotNil(t, proc.proposalTracker)
	proc.beginRound4()
	assert.Nil(t, proc.commitTracker)
	assert.Nil(t, proc.proposalTracker)
}

func TestConsensusProcess_handlePending(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.SetInbox(make(chan *Msg, 100))
	const count = 5
	pending := make(map[string]*Msg)
	for i := 0; i < count; i++ {
		v := generateSigning(t)
		m := BuildStatusMsg(v, NewSetFromValues(value1))
		pending[v.PublicKey().String()] = m
	}
	proc.handlePending(pending)
	assert.Equal(t, count, len(proc.inbox))
}

func TestConsensusProcess_endOfRound3(t *testing.T) {
	proc := generateConsensusProcess(t)
	net := &mockP2p{}
	proc.network = net
	mpt := &mockProposalTracker{}
	proc.proposalTracker = mpt
	mct := &mockCommitTracker{}
	proc.commitTracker = mct
	proc.notifySent = true
	proc.endOfRound3()
	proc.notifySent = false
	assert.Equal(t, 0, mpt.countIsConflicting)
	mpt.isConflicting = true
	proc.endOfRound3()
	assert.Equal(t, 1, mpt.countIsConflicting)
	assert.Equal(t, 0, mct.countHasEnoughCommits)
	mpt.isConflicting = false
	mct.hasEnoughCommits = false
	proc.endOfRound3()
	assert.Equal(t, 0, mct.countBuildCertificate)
	mct.hasEnoughCommits = true
	mct.certificate = nil
	proc.endOfRound3()
	assert.Equal(t, 1, mct.countBuildCertificate)
	assert.Equal(t, 0, mpt.countProposedSet)
	mct.certificate = &Certificate{}
	proc.endOfRound3()
	mpt.proposedSet = nil
	proc.s = NewSmallEmptySet()
	proc.endOfRound3()
	assert.NotNil(t, proc.s)
	mpt.proposedSet = NewSetFromValues(value1)
	proc.endOfRound3()
	assert.True(t, proc.s.Equals(mpt.proposedSet))
	assert.Equal(t, mct.certificate, proc.certificate)
	assert.True(t, proc.notifySent)
}
