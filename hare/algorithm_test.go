package hare

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

var cfg = config.DefaultConfig()

func generateVerifier(t *testing.T) Verifier {
	ms := NewMockSigning()

	return ms.Verifier()
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
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	broker := NewBroker(n1)
	proc := generateConsensusProcess(t)
	broker.Register(proc)
	go proc.eventLoop()
	n2.Broadcast(ProtoName, []byte{})

	proc.Close()
	<-proc.CloseChannel()
}

func TestConsensusProcess_handleMessage(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := NewBroker(n1)
	proc := generateConsensusProcess(t)
	broker.Register(proc)
	m := BuildPreRoundMsg(generateVerifier(t), NewSetFromValues(value1))
	proc.handleMessage(m)
}

func TestConsensusProcess_nextRound(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := NewBroker(n1)
	proc := generateConsensusProcess(t)
	broker.Register(proc)
	proc.advanceToNextRound()
	assert.Equal(t, uint32(1), proc.k)
	proc.advanceToNextRound()
	assert.Equal(t, uint32(2), proc.k)
}

func generateConsensusProcess(t *testing.T) *ConsensusProcess {
	bn, _ := node.GenerateTestNode(t)
	sim := service.NewSimulator()
	n1 := sim.NewNodeFrom(bn.Node)

	s := NewSetFromValues(value1)
	oracle := NewMockHashOracle(numOfClients)
	signing := NewMockSigning()
	oracle.Register(signing.Verifier())
	output := make(chan TerminationOutput,1)

	return NewConsensusProcess(cfg, *instanceId1, s, oracle, signing, n1, output)
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
	oracle := NewMockHashOracle(numOfClients)
	signing := NewMockSigning()

	output := make(chan TerminationOutput,1)
	proc := NewConsensusProcess(cfg, *instanceId1, s, oracle, signing, n1, output)
	broker.Register(proc)

	msg := buildStatusMsg(generateVerifier(t), s, 0)
	proc.sendMessage(msg)
}

func TestConsensusProcess_procPre(t *testing.T) {
	proc := generateConsensusProcess(t)
	s := NewSmallEmptySet()
	m := BuildPreRoundMsg(generateVerifier(t), s)
	proc.processPreRoundMsg(m)
	assert.Equal(t, 1, len(proc.preRoundTracker.preRound))
}

func TestConsensusProcess_procStatus(t *testing.T) {
	proc := generateConsensusProcess(t)
	s := NewSmallEmptySet()
	m := BuildStatusMsg(generateVerifier(t), s)
	proc.processStatusMsg(m)
	assert.Equal(t, 1, len(proc.statusesTracker.statuses))
}

func TestConsensusProcess_procProposal(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	s := NewSmallEmptySet()
	m := BuildProposalMsg(generateVerifier(t), s)
	proc.processProposalMsg(m)
	assert.NotNil(t, proc.proposalTracker.proposal)
}

func TestConsensusProcess_procCommit(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	s := NewSmallEmptySet()
	proc.commitTracker = NewCommitTracker(1, 1, s)
	m := BuildCommitMsg(generateVerifier(t), s)
	proc.processCommitMsg(m)
	assert.Equal(t, 1, len(proc.commitTracker.commits))
}

func TestConsensusProcess_procNotify(t *testing.T) {
	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	s := NewSetFromValues(value1)
	m := BuildNotifyMsg(generateVerifier(t), s)
	proc.processNotifyMsg(m)
	assert.Equal(t, 1, len(proc.notifyTracker.notifies))
}

func TestConsensusProcess_Termination(t *testing.T) {

	proc := generateConsensusProcess(t)
	proc.advanceToNextRound()
	s := NewSetFromValues(value1)

	for i:=0;i<cfg.F+1;i++ {
		proc.processNotifyMsg(BuildNotifyMsg(generateVerifier(t), s))
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

func TestConsensusProcess_onEarlyMessage(t *testing.T) {
	proc := generateConsensusProcess(t)
	m := BuildPreRoundMsg(generateVerifier(t), NewSmallEmptySet())
	proc.advanceToNextRound()
	proc.onEarlyMessage(m)
	assert.Equal(t, 1, len(proc.pending))
	proc.onEarlyMessage(m)
	assert.Equal(t, 1, len(proc.pending))
}