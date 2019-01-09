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
	proc.sendMessage(msg)
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
	s := NewSmallEmptySet()
	m := BuildProposalMsg(generatePubKey(t), s)
	proc.processProposalMsg(m)
	assert.NotNil(t, proc.proposalTracker.proposal)
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
	for i:=0;i<cfg.F+1;i++ {
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