package hare

import (
	"bytes"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

var instanceId0 = InstanceId(0)
var instanceId1 = InstanceId(1)
var instanceId2 = InstanceId(2)
var instanceId3 = InstanceId(3)

type mockClient struct {
	id InstanceId
}

type MockStateQuerier struct {
	res bool
	err error
}

func NewMockStateQuerier() MockStateQuerier {
	return MockStateQuerier{true, nil}
}

func (msq MockStateQuerier) IsIdentityActive(edId string, layer types.LayerID) (bool, error) {
	return msq.res, msq.err
}

func createMessage(t *testing.T, instanceId InstanceId) []byte {
	sr := signing.NewEdSigner()
	b := NewMessageBuilder()
	msg := b.SetPubKey(sr.PublicKey()).SetInstanceId(instanceId).Sign(sr).Build()

	var w bytes.Buffer
	_, err := xdr.Marshal(&w, msg.Message)

	if err != nil {
		assert.Fail(t, "Failed to marshal data")
	}

	return w.Bytes()
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

// test that a InnerMsg to a specific set Id is delivered by the broker
func TestBroker_Received(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	broker := buildBroker(n1)
	broker.Start()

	inbox := broker.Register(instanceId1)

	serMsg := createMessage(t, instanceId1)
	n2.Broadcast(protoName, serMsg)

	waitForMessages(t, inbox, instanceId1, 1)
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

func waitForMessages(t *testing.T, inbox chan *Msg, instanceId InstanceId, msgCount int) {
	i := 0
	for {
		tm := time.NewTimer(3 * time.Second)
		for {
			select {
			case x := <-inbox:
				assert.True(t, x.InnerMsg.InstanceId == instanceId)
				i++
				if i >= msgCount {
					return
				}
			case <-tm.C:
				t.Errorf("Timedout waiting for msg %v", i)
				t.Fail()
				return
			}
		}

	}
}

// test flow for multiple set objectId
func TestBroker_MultipleInstanceIds(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	const msgCount = 1

	broker := buildBroker(n1)
	broker.Start()

	inbox1 := broker.Register(instanceId1)
	inbox2 := broker.Register(instanceId2)
	inbox3 := broker.Register(instanceId3)

	go sendMessages(t, instanceId1, n2, msgCount)
	go sendMessages(t, instanceId2, n2, msgCount)
	go sendMessages(t, instanceId3, n2, msgCount)

	go waitForMessages(t, inbox1, instanceId1, msgCount)
	go waitForMessages(t, inbox2, instanceId2, msgCount)
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
	msg    *Msg
	sender p2pcrypto.PublicKey
	vComp  chan service.MessageValidation
}

func (mgm *mockGossipMessage) Bytes() []byte {
	var w bytes.Buffer
	_, err := xdr.Marshal(&w, mgm.msg.Message)
	if err != nil {
		log.Error("Could not marshal err=%v", err)
		return nil
	}

	return w.Bytes()
}

func (mgm *mockGossipMessage) ValidationCompletedChan() chan service.MessageValidation {
	return mgm.vComp
}

func (mgm *mockGossipMessage) Sender() p2pcrypto.PublicKey {
	return mgm.sender
}

func (mgm *mockGossipMessage) ReportValidation(protocol string) {
	mgm.vComp <- service.NewMessageValidation(mgm.sender, nil, "")
}

func newMockGossipMsg(msg *Message) *mockGossipMessage {
	return &mockGossipMessage{&Msg{msg, nil}, p2pcrypto.NewRandomPubkey(), make(chan service.MessageValidation, 10)}
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

	msg := BuildPreRoundMsg(signing.NewEdSigner(), NewSetFromValues(value1)).Message
	msg.InnerMsg.InstanceId = 2
	m = newMockGossipMsg(msg)
	broker.inbox <- m

	msg.InnerMsg.InstanceId = 1
	m = newMockGossipMsg(msg)
	broker.inbox <- m
	// nothing happens since this is an  invalid InnerMsg

	mev.valid = true
	broker.inbox <- m
	assertMsg(t, m)
}

func TestBroker_Register(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := buildBroker(n1)
	broker.Start()
	msg := BuildPreRoundMsg(signing.NewEdSigner(), NewSetFromValues(value1))
	broker.pending[instanceId1] = []*Msg{msg, msg}
	broker.Register(instanceId1)
	assert.Equal(t, 2, len(broker.outbox[instanceId1]))
	assert.Equal(t, 0, len(broker.pending[instanceId1]))
}

func assertMsg(t *testing.T, msg *mockGossipMessage) {
	tm := time.NewTimer(2 * time.Second)
	select {
	case <-tm.C:
		t.Error("Timeout")
		t.FailNow()
	case <-msg.ValidationCompletedChan():
		return
	}
}

func TestBroker_Register2(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := buildBroker(n1)
	broker.Start()
	broker.Register(instanceId1)
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewSetFromValues(value1)).Message
	m.InnerMsg.InstanceId = instanceId1
	msg := newMockGossipMsg(m)
	broker.inbox <- msg
	assertMsg(t, msg)
	m.InnerMsg.InstanceId = instanceId2
	msg = newMockGossipMsg(m)
	broker.inbox <- msg
	assertMsg(t, msg)
}

func TestBroker_Register3(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := buildBroker(n1)
	broker.Start()

	m := BuildPreRoundMsg(signing.NewEdSigner(), NewSetFromValues(value1)).Message
	m.InnerMsg.InstanceId = instanceId0
	msg := newMockGossipMsg(m)
	broker.inbox <- msg
	time.Sleep(1)
	client := mockClient{instanceId0}
	ch := broker.Register(client.id)
	timer := time.NewTimer(2 * time.Second)
	for {
		select {
		case <-ch:
			return
		case <-timer.C:
			t.FailNow()
		}

	}
}

func TestBroker_PubkeyExtraction(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := buildBroker(n1)
	broker.Start()
	inbox := broker.Register(instanceId1)
	sgn := signing.NewEdSigner()
	m := BuildPreRoundMsg(sgn, NewSetFromValues(value1)).Message
	m.InnerMsg.InstanceId = instanceId1
	msg := newMockGossipMsg(m)
	broker.inbox <- msg
	tm := time.NewTimer(2 * time.Second)
	for {
		select {
		case inMsg := <-inbox:
			assert.True(t, sgn.PublicKey().Equals(inMsg.PubKey))
			return
		case <-tm.C:
			t.Error("Timeout")
			t.FailNow()
			return
		}
	}
}

func Test_newMsg(t *testing.T) {
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewSetFromValues(value1)).Message
	// TODO: remove this comment when ready
	//_, e := newMsg(m, MockStateQuerier{false, errors.New("my err")})
	//assert.NotNil(t, e)
	_, e := newMsg(m, MockStateQuerier{true, nil}, 10)
	assert.Nil(t, e)
}
