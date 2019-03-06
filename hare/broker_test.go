package hare

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

var instanceId0 = InstanceId(0)
var instanceId1 = InstanceId(1)
var instanceId2 = InstanceId(2)
var instanceId3 = InstanceId(3)

func createMessage(t *testing.T, instanceId InstanceId) []byte {
	hareMsg := &pb.HareMessage{}
	hareMsg.Message = &pb.InnerMessage{InstanceId: uint32(instanceId)}
	serMsg, err := proto.Marshal(hareMsg)

	if err != nil {
		assert.Fail(t, "Failed to marshal data")
	}

	return serMsg
}

func TestBroker_Start(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := buildBroker(n1)

	err := broker.Start()
	assert.Nil(t, err)

	err = broker.Start()
	assert.NotNil(t, err)
	assert.Equal(t, "instance already started", err.Error())
}

// test that a message to a specific set Id is delivered by the broker
func TestBroker_Received(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	broker := buildBroker(n1)
	broker.Start()

	inbox := broker.Register(instanceId1)

	serMsg := createMessage(t, instanceId1)
	n2.Broadcast(protoName, serMsg)

	recv := <-inbox

	assert.True(t, recv.Message.InstanceId == uint32(instanceId1))
}

// test that aborting the broker aborts
func TestBroker_Abort(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	broker := buildBroker(n1)
	broker.Start()

	timer := time.NewTimer(3 * time.Second)

	go broker.Close()

	select {
	case <-broker.CloseChannel():
		assert.True(t, true)
	case <-timer.C:
		assert.Fail(t, "timeout")
	}
}

func sendMessages(t *testing.T, instanceId InstanceId, n *service.Node, count int) {
	for i := 0; i < count; i++ {
		n.Broadcast(protoName, createMessage(t, instanceId))
	}
}

func waitForMessages(t *testing.T, inbox chan *pb.HareMessage, instanceId InstanceId, msgCount int) {
	for i := 0; i < msgCount; i++ {
		x := <-inbox
		assert.True(t, x.Message.InstanceId == uint32(instanceId))
	}
}

// test flow for multiple set objectId
func TestBroker_MultipleInstanceIds(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	const msgCount = 100

	broker := buildBroker(n1)
	broker.Start()

	inbox1 := broker.Register(instanceId1)
	inbox2 := broker.Register(instanceId2)
	inbox3 := broker.Register(instanceId3)

	go sendMessages(t, instanceId1, n2, msgCount)
	go sendMessages(t, instanceId2, n2, msgCount)
	go sendMessages(t, instanceId3, n2, msgCount)

	waitForMessages(t, inbox1, instanceId1, msgCount)
	waitForMessages(t, inbox2, instanceId2, msgCount)
	waitForMessages(t, inbox3, instanceId3, msgCount)

	assert.True(t, true)
}

func TestBroker_RegisterUnregister(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := buildBroker(n1)
	broker.Start()
	broker.Register(instanceId1)
	assert.Equal(t, 1, len(broker.outbox))
	broker.Unregister(instanceId1)
	assert.Equal(t, 0, len(broker.outbox))
}

type mockGossipMessage struct {
	msg            *pb.HareMessage
	lastValidation bool
	vComp          chan service.MessageValidation
}

func (mgm *mockGossipMessage) Bytes() []byte {
	data, err := proto.Marshal(mgm.msg)
	if err != nil {
		return nil
	}

	return data
}

func (mgm *mockGossipMessage) ValidationCompletedChan() chan service.MessageValidation {
	return mgm.vComp
}

func (mgm *mockGossipMessage) ReportValidation(protocol string, isValid bool) {
	mgm.lastValidation = isValid
	mgm.vComp <- service.MessageValidation{}
}

func newMockGossipMsg(msg *pb.HareMessage) *mockGossipMessage {
	return &mockGossipMessage{msg, false, make(chan service.MessageValidation, 10)}
}

func TestBroker_Send(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := buildBroker(n1)
	mev := &mockEligibilityValidator{false}
	broker.eValidator = mev
	broker.Start()

	m := newMockGossipMsg(nil)
	broker.inbox <- m
	<-m.ValidationCompletedChan()
	assert.False(t, m.lastValidation)

	msg := BuildPreRoundMsg(NewMockSigning(), NewSetFromValues(value1))
	msg.Message.InstanceId = 2
	m = newMockGossipMsg(msg)
	broker.inbox <- m
	<-m.ValidationCompletedChan()
	assert.False(t, m.lastValidation)

	msg.Message.InstanceId = 1
	m = newMockGossipMsg(msg)
	broker.inbox <- m
	<-m.ValidationCompletedChan()
	assert.False(t, m.lastValidation)

	mev.valid = true
	broker.inbox <- m
	<-m.ValidationCompletedChan()
	assert.True(t, m.lastValidation)
}

func TestBroker_Register(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := buildBroker(n1)
	broker.Start()
	msg := BuildPreRoundMsg(NewMockSigning(), NewSetFromValues(value1))
	broker.pending[instanceId1] = []*pb.HareMessage{msg, msg}
	broker.Register(instanceId1)
	assert.Equal(t, 2, len(broker.outbox[instanceId1]))
	assert.Equal(t, 0, len(broker.pending[instanceId1]))
}
