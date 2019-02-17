package hare

import (
	"bytes"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

var cfg = config.Config{N: 10, F: 5, SetSize: 10, RoundDuration: time.Second * time.Duration(2)}

type mockMessageValidator struct {
	syntaxValid   bool
	contextValid  bool
	contextValid2 bool
	countSyntax   int
	countContext  int
	firstK        int32
}

func (mmv *mockMessageValidator) SyntacticallyValidateMessage(m *pb.HareMessage) bool {
	mmv.countSyntax++
	return mmv.syntaxValid
}

func (mmv *mockMessageValidator) ContextuallyValidateMessage(m *pb.HareMessage, expectedK int32) bool {
	mmv.countContext++
	if mmv.firstK == expectedK {
		return mmv.contextValid
	}

	return mmv.contextValid2
}

type mockRolacle struct {
	isEligible bool
}

func (mr *mockRolacle) Eligible(instanceID uint32, committeeSize int, pubKey string, proof []byte) bool {
	return mr.isEligible
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

func (mpt *mockProposalTracker) OnProposal(msg *pb.HareMessage) {
	mpt.countOnProposal++
}

func (mpt *mockProposalTracker) OnLateProposal(msg *pb.HareMessage) {
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
	certificate           *pb.Certificate
}

func (mct *mockCommitTracker) OnCommit(msg *pb.HareMessage) {
	mct.countOnCommit++
}

func (mct *mockCommitTracker) HasEnoughCommits() bool {
	mct.countHasEnoughCommits++
	return mct.hasEnoughCommits
}

func (mct *mockCommitTracker) BuildCertificate() *pb.Certificate {
	mct.countBuildCertificate++
	return mct.certificate
}

func generateSigning(t *testing.T) Signing {
	return NewMockSigning()
}

func buildMessage(msg *pb.HareMessage) Message {
	data, err := proto.Marshal(msg)
	if err != nil {
		log.Error("failed marshaling message")
	}
	return Message{msg, data, make(chan service.MessageValidation, 10)}
}

func assertValidation(t *testing.T, msg Message, expValid bool, expProt string) {
	timer := time.NewTimer(time.Second * 1)
	select {
	case act := <-msg.validationChan:
		assert.Equal(t, expValid, act.IsValid())
		assert.Equal(t, msg.bytes, act.Message())
		assert.Equal(t, expProt, act.Protocol())
	case <-timer.C:
		t.FailNow() // deadlocked
	}
}

// test that a message to a specific set id is delivered by the broker
func TestConsensusProcess_StartTwice(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := NewBroker(n1)
	proc := generateConsensusProcess(t)
	broker.Register(proc)
	err := proc.Start()
	assert.Equal(t, nil, err)
	err = proc.Start()
	assert.Equal(t, "instance already started", err.Error())
}

func TestConsensusProcess_eventLoop(t *testing.T) {
	// TODO fix! This test does nothing.. the message that are Broadcast isn't handled because there are no registered protocol handler
	net := &mockP2p{}
	broker := NewBroker(net)
	proc := generateConsensusProcess(t)
	proc.network = net
	oracle := &mockRolacle{}
	proc.oracle = oracle
	oracle.isEligible = true
	broker.Register(proc)
	proc.s = NewSetFromValues(value1, value2)
	proc.cfg.F = 2
	go proc.eventLoop()
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 1, net.count)
}

func TestConsensusProcess_handleMessage(t *testing.T) {
	net := &mockP2p{}
	broker := NewBroker(net)
	proc := generateConsensusProcess(t)
	proc.network = net
	oracle := &mockRolacle{}
	proc.oracle = oracle
	mValidator := &mockMessageValidator{}
	mValidator.firstK = proc.k
	proc.validator = mValidator
	broker.Register(proc)
	m := BuildPreRoundMsg(generateSigning(t), NewSetFromValues(value1))
	msg := buildMessage(m)
	oracle.isEligible = false
	proc.handleMessage(msg)
	assertValidation(t, msg, false, ProtoName)
	oracle.isEligible = true
	mValidator.syntaxValid = false
	proc.handleMessage(msg)
	assertValidation(t, msg, false, ProtoName)
	assert.Equal(t, 1, mValidator.countSyntax)
	mValidator.syntaxValid = true
	proc.handleMessage(msg)
	assertValidation(t, msg, true, ProtoName)
	assert.NotEqual(t, 0, mValidator.countContext)
	mValidator.contextValid = true
	proc.handleMessage(msg)
	assertValidation(t, msg, true, ProtoName)
	assert.Equal(t, 0, len(proc.pending))
	mValidator.contextValid = false
	proc.handleMessage(msg)
	assert.Equal(t, 0, len(proc.pending))
	mValidator.contextValid = false
	mValidator.contextValid2 = true
	proc.handleMessage(msg)
	assert.Equal(t, 1, len(proc.pending))
}

func TestConsensusProcess_nextRound(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := NewBroker(n1)
	proc := generateConsensusProcess(t)
	broker.Register(proc)
	proc.advanceToNextRound()
	proc.advanceToNextRound()
	assert.Equal(t, int32(1), proc.k)
	proc.advanceToNextRound()
	assert.Equal(t, int32(2), proc.k)
}

func generateConsensusProcess(t *testing.T) *ConsensusProcess {
	bn, _ := node.GenerateTestNode(t)
	sim := service.NewSimulator()
	n1 := sim.NewNodeFrom(bn.Node)

	s := NewSetFromValues(value1)
	oracle := NewMockHashOracle(numOfClients)
	signing := NewMockSigning()
	oracle.Register(signing.Verifier().String())
	output := make(chan TerminationOutput, 1)

	return NewConsensusProcess(cfg, *instanceId1, s, oracle, signing, n1, output, log.NewDefault(signing.Verifier().String()))
}

func TestConsensusProcess_Id(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.instanceId = *instanceId1
	assert.Equal(t, instanceId1.Id(), proc.Id())
}

func TestNewConsensusProcess_AdvanceToNextRound(t *testing.T) {
	proc := generateConsensusProcess(t)
	k := proc.k
	proc.advanceToNextRound()
	assert.Equal(t, k+1, proc.k)
}

func TestConsensusProcess_CreateInbox(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.createInbox(100)
	assert.NotNil(t, proc.inbox)
	assert.Equal(t, 100, cap(proc.inbox))
}

func TestConsensusProcess_InitDefaultBuilder(t *testing.T) {
	proc := generateConsensusProcess(t)
	s := NewEmptySet(cfg.SetSize)
	s.Add(value1)
	builder := proc.initDefaultBuilder(s)
	assert.True(t, NewSet(builder.inner.Values).Equals(s))
	verifier, _ := NewVerifier(builder.outer.PubKey)
	assert.Equal(t, verifier.Bytes(), proc.signing.Verifier().Bytes())
	assert.Equal(t, builder.inner.K, proc.k)
	assert.Equal(t, builder.inner.Ki, proc.ki)
	assert.Equal(t, builder.inner.InstanceId, proc.instanceId.Bytes())
}

func TestConsensusProcess_Proof(t *testing.T) {
	proc := generateConsensusProcess(t)
	prev := make([]byte, 0)
	for i := 0; i < lowThresh10; i++ {
		assert.False(t, bytes.Equal(proc.roleProof(), prev))
		prev = proc.roleProof()
		proc.advanceToNextRound()
	}
}

func TestConsensusProcess_isEligible(t *testing.T) {
	proc := generateConsensusProcess(t)
	oracle := &mockRolacle{}
	proc.oracle = oracle
	oracle.isEligible = false
	assert.False(t, proc.isEligible())
	oracle.isEligible = true
	assert.True(t, proc.isEligible())
}

func TestConsensusProcess_sendMessage(t *testing.T) {
	net := &mockP2p{}
	oracle := &mockRolacle{}

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

func TestConsensusProcess_validateRole(t *testing.T) {
	proc := generateConsensusProcess(t)
	oracle := &mockRolacle{}
	proc.oracle = oracle
	assert.False(t, proc.validateRole(nil))
	m := BuildPreRoundMsg(generateSigning(t), NewSmallEmptySet())
	m.Message = nil
	assert.False(t, proc.validateRole(m))
	m = BuildPreRoundMsg(generateSigning(t), NewSmallEmptySet())
	oracle.isEligible = false
	assert.False(t, proc.validateRole(m))
	oracle.isEligible = true
	assert.True(t, proc.validateRole(m))
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
	proc.advanceToNextRound()
	proc.advanceToNextRound()
	s := NewSmallEmptySet()
	m := BuildProposalMsg(generateSigning(t), s)
	mpt := &mockProposalTracker{}
	proc.proposalTracker = mpt
	proc.processProposalMsg(m)
	assert.Equal(t, 1, mpt.countOnProposal)
	proc.advanceToNextRound()
	proc.processProposalMsg(m)
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
	m.Message.K = proc.ki
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
	msg := BuildPreRoundMsg(generateSigning(t), NewSmallEmptySet())
	m := buildMessage(msg)
	proc.advanceToNextRound()
	proc.onEarlyMessage(buildMessage(nil))
	assert.Equal(t, 0, len(proc.pending))
	proc.onEarlyMessage(m)
	assert.Equal(t, 1, len(proc.pending))
	proc.onEarlyMessage(m)
	assert.Equal(t, 1, len(proc.pending))
}

func TestProcOutput_Id(t *testing.T) {
	po := procOutput{*instanceId1, nil}
	assert.Equal(t, po.Id(), instanceId1.Bytes())
}

func TestProcOutput_Set(t *testing.T) {
	es := NewSmallEmptySet()
	po := procOutput{*instanceId1, es}
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
	oracle := &mockRolacle{}
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
	oracle := &mockRolacle{}
	proc.oracle = oracle
	oracle.isEligible = true

	statusTracker := NewStatusTracker(1, 1)
	s := NewSetFromValues(value1)
	statusTracker.RecordStatus(BuildStatusMsg(generateSigning(t), s))
	statusTracker.analyzed = true
	proc.statusesTracker = statusTracker

	proc.k = 1
	proc.createInbox(1)
	proc.beginRound2()

	assert.Equal(t, 1, network.count)
	assert.Nil(t, proc.statusesTracker)
}

func TestConsensusProcess_beginRound3(t *testing.T) {
	proc := generateConsensusProcess(t)
	network := &mockP2p{}
	proc.network = network
	oracle := &mockRolacle{}
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
	proc.createInbox(1)
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
	proc.createInbox(100)
	const count = 5
	pending := make(map[string]Message)
	for i := 0; i < count; i++ {
		v := generateSigning(t)
		msg := BuildStatusMsg(v, NewSetFromValues(value1))
		m := buildMessage(msg)
		pending[v.Verifier().String()] = m
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
	mct.certificate = &pb.Certificate{}
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
