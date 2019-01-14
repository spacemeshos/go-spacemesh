package hare

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

var cfg = config.DefaultConfig()

func generatePubKey(t *testing.T) crypto.PublicKey {
	_, pub, err := crypto.GenerateKeyPair()

	if err != nil {
		assert.Fail(t, "failed generating key")
		t.FailNow()
	}

	return pub
}

// test that a message to a specific set id is delivered by the broker
func TestConsensusProcess_StartTwice(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	broker := NewBroker(n1)
	s := NewEmptySet(cfg.SetSize)
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(cfg, generatePubKey(t), *instanceId1, *s, oracle, signing, n1)
	broker.Register(proc)
	err := proc.Start()
	assert.Equal(t, nil, err)
	err = proc.Start()
	assert.Equal(t, "instance already started", err.Error())
}

func TestConsensusProcess_eventLoop(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	broker := NewBroker(n1)
	s := NewEmptySet(cfg.SetSize)
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(cfg, generatePubKey(t), *instanceId1, *s, oracle, signing, n1)
	broker.Register(proc)
	go proc.eventLoop()
	n2.Broadcast(ProtoName, []byte{})

	proc.Close()
	<-proc.CloseChannel()
}

func TestConsensusProcess_eventLoop_roundGo(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.cfg.RoundDuration = time.Second

	go proc.eventLoop()

	ticker := time.NewTicker(time.Second)
	count := 0

Loop:
	for {
		select {
		case <-ticker.C:
			count += 1
			if count > 6 {
				proc.Close()
				break Loop
			}
			if count > 2 {
				assert.True(t, proc.k > Round1)
			}
			if count > 3 {
				assert.True(t, proc.k > Round2)
			}
			if count > 4 {
				assert.True(t, proc.k > Round3)
			}
		}
	}
}

func TestConsensusProcess_eventLoop_terminate(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.cfg.RoundDuration = time.Second

	go proc.eventLoop()
	proc.terminating = true

	ticker := time.NewTicker(time.Second)
	count := 0
Loop:
	for {
		select {
		case <-ticker.C:
			count += 1
			if count > 2 {
				break Loop
			}
		}
	}

	assert.True(t, proc.k == 1)
}

func TestConsensusProcess_eventLoop_handleMessage(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.cfg.RoundDuration = time.Second
	proc.createInbox(1)

	go proc.eventLoop()

	s := NewSmallEmptySet()
	m := BuildPreRoundMsg(generatePubKey(t), s)
	proc.role = Active
	proc.sendMessage(m)

	proc.terminating = true

	assert.Equal(t, 0, len(proc.preRoundTracker.preRound))
}

func TestConsensusProcess_handleMessage(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	broker := NewBroker(n1)
	s := NewEmptySet(cfg.SetSize)
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(cfg, generatePubKey(t), *instanceId1, *s, oracle, signing, n1)
	broker.Register(proc)

	m := NewMessageBuilder().SetRoundCounter(0).SetInstanceId(*instanceId1).SetPubKey(generatePubKey(t)).Sign(proc.signing).Build()

	proc.handleMessage(m)
}

func TestConsensusProcess_nextRound(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	broker := NewBroker(n1)
	s := NewEmptySet(cfg.SetSize)
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(cfg, generatePubKey(t), *instanceId1, *s, oracle, signing, n1)
	broker.Register(proc)

	proc.advanceToNextRound()
	assert.Equal(t, uint32(1), proc.k)
	proc.advanceToNextRound()
	assert.Equal(t, uint32(2), proc.k)
}

func generateConsensusProcess(t *testing.T) *ConsensusProcess {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	s := NewEmptySet(cfg.SetSize)
	oracle := NewMockOracle()
	signing := NewMockSigning()

	return NewConsensusProcess(cfg, generatePubKey(t), *instanceId1, *s, oracle, signing, n1)
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
	pub, err := crypto.NewPublicKey(builder.outer.PubKey)
	assert.Nil(t, err)
	assert.Equal(t, pub.Bytes(), proc.pubKey.Bytes())
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

func TestConsensusProcess_roleFromRoundCounter(t *testing.T) {
	r := uint32(0)
	for i := 0; i < 10; i++ {
		assert.Equal(t, Active, roleFromRoundCounter(r))
		r++
		assert.Equal(t, Leader, roleFromRoundCounter(r))
		r++
		assert.Equal(t, Active, roleFromRoundCounter(r))
		r++
		assert.Equal(t, Passive, roleFromRoundCounter(r))
		r++
	}
}

func TestConsensusProcess_sendMessage(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := NewBroker(n1)
	s := NewEmptySet(cfg.SetSize)
	oracle := NewMockOracle()
	signing := NewMockSigning()

	proc := NewConsensusProcess(cfg, generatePubKey(t), *instanceId1, *s, oracle, signing, n1)
	broker.Register(proc)

	msg := buildStatusMsg(generatePubKey(t), s, 0)
	proc.createInbox(1)
	proc.sendMessage(msg)

	proc.advanceToNextRound()
	proc.advanceToNextRound()
	proc.advanceToNextRound()
	proc.sendMessage(msg)

	assertInBoxHasMessage(t, proc)
}

func TestConsensusProcess_sendMessage_Nil(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.createInbox(1)

	proc.sendMessage(nil)
	assertInBoxHasNoMessage(t, proc)
}

func assertInBoxHasNoMessage(t *testing.T, proc *ConsensusProcess) {
	timer := time.NewTimer(time.Second)
Loop:
	for {
		select {
		case <-proc.inbox:
			assert.True(t, false)
			break Loop
		case <-timer.C:
			assert.True(t, true) // it should reach
			break Loop
		}
	}
}

func TestConsensusProcess_procPre(t *testing.T) {
	proc := generateConsensusProcess(t)
	s := NewSmallEmptySet()
	m := BuildPreRoundMsg(generatePubKey(t), s)
	proc.processPreRoundMsg(m)
	assert.Equal(t, 1, len(proc.preRoundTracker.preRound))
}

func TestConsensusProcess_procStatus(t *testing.T) {
	proc := generateConsensusProcess(t)
	s := NewSmallEmptySet()
	m := BuildStatusMsg(generatePubKey(t), s)
	proc.processStatusMsg(m)
	assert.Equal(t, 1, len(proc.statusesTracker.statuses))
}

func TestConsensusProcess_procProposal(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()

	pubKey := generatePubKey(t)
	s1 := NewSetFromValues(value1)
	m1 := BuildProposalMsg(pubKey, s1)
	proc.processProposalMsg(m1)
	assert.NotEqual(t, proc.proposalTracker.proposal, m1)

	s := NewSmallEmptySet()
	m := BuildProposalMsg(pubKey, s)
	proc.processProposalMsg(m)
	assert.NotNil(t, proc.proposalTracker.proposal)

	proc.advanceToNextRound()
	proc.processProposalMsg(m)
	assert.Equal(t, proc.proposalTracker.proposal, m)
}

func TestConsensusProcess_procCommit(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	s := NewSmallEmptySet()
	proc.commitTracker = NewCommitTracker(1, 1, s)
	m := BuildCommitMsg(generatePubKey(t), s)
	proc.processCommitMsg(m)
	assert.Equal(t, 1, len(proc.commitTracker.commits))
}

func TestConsensusProcess_procNotify(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	s := NewSmallEmptySet()
	m := BuildNotifyMsg(generatePubKey(t), s)
	proc.processNotifyMsg(m)
	assert.Equal(t, 1, len(proc.notifyTracker.notifies))
}

func TestConsensusProcess_Termination(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	s := NewSetFromValues(value1)
	for i := 0; i < cfg.F+1; i++ {
		proc.processNotifyMsg(BuildNotifyMsg(generatePubKey(t), s))
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
	assert.Equal(t, Round1, proc.currentRound())
	proc.advanceToNextRound()
	assert.Equal(t, Round2, proc.currentRound())
	proc.advanceToNextRound()
	assert.Equal(t, Round3, proc.currentRound())
	proc.advanceToNextRound()
	assert.Equal(t, Round4, proc.currentRound())
}

func TestConsensusProcess_beginRound4(t *testing.T) {
	proc := generateConsensusProcess(t)
	assert.NotNil(t, proc.commitTracker)
	assert.NotNil(t, proc.proposalTracker)
	proc.beginRound4()
	assert.Nil(t, proc.commitTracker)
	assert.Nil(t, proc.proposalTracker)
}

func TestConsensusProcess_beginRound3(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.commitTracker.proposedSet = NewSetFromValues(value1)

	preCommitTracker := proc.commitTracker
	proc.beginRound3()
	assert.NotEqual(t, preCommitTracker, proc.commitTracker)

	s := NewSmallEmptySet()
	proc.proposalTracker.proposal = BuildProposalMsg(generatePubKey(t), s)
	proc.role = Active
	proc.createInbox(1)
	proc.beginRound3()

	assertInBoxHasMessage(t, proc)
}

func assertInBoxHasMessage(t *testing.T, proc *ConsensusProcess) {
	timer := time.NewTimer(time.Second)
Loop:
	for {
		select {
		case msg := <-proc.inbox:
			assert.NotNil(t, msg)
			break Loop
		case <-timer.C:
			assert.True(t, false) // it should not reach
			break Loop
		}
	}
}

func TestConsensusProcess_beginRound1(t *testing.T) {
	proc := generateConsensusProcess(t)
	s := NewSmallEmptySet()
	m := BuildPreRoundMsg(generatePubKey(t), s)
	proc.statusesTracker.RecordStatus(m)

	preStatusTracker := proc.statusesTracker
	proc.beginRound1()
	assert.NotEqual(t, preStatusTracker, proc.statusesTracker)
}

func TestConsensusProcess_endOfRound3(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.endOfRound3()

	assert.Empty(t, proc.s.values)

	s := NewSetFromValues(value1)
	commitTracker := NewCommitTracker(2, 2, s)
	commitTracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	commitTracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	proc.commitTracker = commitTracker

	pubKey := generatePubKey(t)
	m1 := buildProposalMsg(pubKey, s, []byte{1})

	proposalTracker := NewProposalTracker(lowThresh10)
	proposalTracker.OnProposal(m1)

	proc.endOfRound3()
	assert.Empty(t, proc.s.values)

	proc.proposalTracker = proposalTracker

	proc.endOfRound3()
	assert.NotEmpty(t, proc.s.values)
}

func TestConsensusProcess_endOfRound3_proposalTrackerConflict(t *testing.T) {
	proc := generateConsensusProcess(t)

	s := NewSetFromValues(value1)
	commitTracker := NewCommitTracker(2, 2, s)
	commitTracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	commitTracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	proc.commitTracker = commitTracker

	pubKey := generatePubKey(t)
	m1 := buildProposalMsg(pubKey, s, []byte{1})

	proposalTracker := NewProposalTracker(lowThresh10)
	proposalTracker.OnProposal(m1)

	s.Add(value2)
	m2 := buildProposalMsg(pubKey, s, []byte{1})
	proposalTracker.OnProposal(m2)
	proc.proposalTracker = proposalTracker

	proc.endOfRound3()
	assert.Empty(t, proc.s.values)
}

func TestIterationFromCounter(t *testing.T) {
	assert.Equal(t, iterationFromCounter(4), uint32(1))
}

func TestConsensusProcess_beginRound2(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.role = Leader

	statusTracker := NewStatusTracker(1, 1)
	s := NewSetFromValues(value1)
	statusTracker.RecordStatus(BuildStatusMsg(generatePubKey(t), s))
	proc.statusesTracker = statusTracker

	proc.k = 1
	proc.createInbox(1)
	proc.beginRound2()

	assertInBoxHasMessage(t, proc)
	assert.Nil(t, proc.statusesTracker)
}

func TestConsensusProcess_onRoundEnd(t *testing.T) {
	proc := generateConsensusProcess(t)

	s := NewSetFromValues(value1)
	commitTracker := NewCommitTracker(2, 2, s)
	commitTracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	commitTracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	proc.commitTracker = commitTracker

	pubKey := generatePubKey(t)
	m1 := buildProposalMsg(pubKey, s, []byte{1})

	proposalTracker := NewProposalTracker(lowThresh10)
	proposalTracker.OnProposal(m1)

	proc.proposalTracker = proposalTracker
	proc.k = 1
	proc.onRoundEnd()
	assert.Empty(t, proc.s.values)

	proc.k = 2
	proc.onRoundEnd()

	assert.NotEmpty(t, proc.s.values)
}

func TestConsensusProcess_onRoundBegin(t *testing.T) {
	proc := generateConsensusProcess(t)
	s := NewSmallEmptySet()
	m := BuildPreRoundMsg(generatePubKey(t), s)
	proc.statusesTracker.RecordStatus(m)

	preStatusTracker := proc.statusesTracker
	proc.onRoundBegin()
	assert.NotEqual(t, preStatusTracker, proc.statusesTracker) // round 1 begin

	proc.advanceToNextRound()

	proc.statusesTracker = NewStatusTracker(1, 1)
	proc.statusesTracker.RecordStatus(m)
	proc.createInbox(1)
	proc.onRoundBegin()
	assert.Nil(t, proc.statusesTracker) // round 2 begin

	proc.advanceToNextRound()
	proc.commitTracker.proposedSet = NewSetFromValues(value1)
	preCommitTracker := proc.commitTracker
	proc.onRoundBegin()
	assert.NotEqual(t, preCommitTracker, proc.commitTracker) // round 3 begin

	proc.advanceToNextRound()
	proc.onRoundBegin()
	assert.Nil(t, proc.commitTracker) // round 4 begin
}
