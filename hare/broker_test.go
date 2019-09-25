package hare

import (
	"bytes"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

var instanceId0 = instanceId(0)
var instanceId1 = instanceId(1)
var instanceId2 = instanceId(2)
var instanceId3 = instanceId(3)
var instanceId4 = instanceId(4)
var instanceId5 = instanceId(5)
var instanceId6 = instanceId(6)
var instanceId7 = instanceId(7)

func trueFunc() bool {
	return true
}

func falseFunc() bool {
	return false
}

type mockClient struct {
	id instanceId
}

type MockStateQuerier struct {
	res bool
	err error
}

func NewMockStateQuerier() MockStateQuerier {
	return MockStateQuerier{true, nil}
}

func (msq MockStateQuerier) IsIdentityActiveOnConsensusView(edId string, layer types.LayerID) (bool, error) {
	return msq.res, msq.err
}

func createMessage(t *testing.T, instanceId instanceId) []byte {
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
	broker := buildBroker(n1, t.Name())

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

	broker := buildBroker(n1, t.Name())
	broker.Start()

	inbox, err := broker.Register(instanceId1)
	assert.Nil(t, err)

	serMsg := createMessage(t, instanceId1)
	n2.Broadcast(protoName, serMsg)
	waitForMessages(t, inbox, instanceId1, 1)
}

// test that aborting the broker aborts
func TestBroker_Abort(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	broker := buildBroker(n1, t.Name())
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

func sendMessages(t *testing.T, instanceId instanceId, n *service.Node, count int) {
	for i := 0; i < count; i++ {
		n.Broadcast(protoName, createMessage(t, instanceId))
	}
}

func waitForMessages(t *testing.T, inbox chan *Msg, instanceId instanceId, msgCount int) {
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

	broker := buildBroker(n1, t.Name())
	broker.Start()

	inbox1, _ := broker.Register(instanceId1)
	inbox2, _ := broker.Register(instanceId2)
	inbox3, _ := broker.Register(instanceId3)

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
	broker := buildBroker(n1, t.Name())
	broker.Start()
	broker.Register(instanceId1)
	assert.Equal(t, 1, len(broker.outbox))
	broker.Unregister(instanceId1)
	assert.Nil(t, broker.outbox[instanceId1])
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
	broker := buildBroker(n1, t.Name())
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
	broker := buildBroker(n1, t.Name())
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
	broker := buildBroker(n1, t.Name())
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
	broker := buildBroker(n1, t.Name())
	broker.Start()

	m := BuildPreRoundMsg(signing.NewEdSigner(), NewSetFromValues(value1)).Message
	m.InnerMsg.InstanceId = instanceId1
	msg := newMockGossipMsg(m)
	broker.inbox <- msg
	time.Sleep(1)
	client := mockClient{instanceId1}
	ch, _ := broker.Register(client.id)
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
	broker := buildBroker(n1, t.Name())
	broker.Start()
	inbox, _ := broker.Register(instanceId1)
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

func TestBroker_updateInstance(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())
	r.Equal(instanceId(0), b.latestLayer)
	b.updateLatestLayer(1)
	r.Equal(instanceId(1), b.latestLayer)
	b.updateLatestLayer(2)
	r.Equal(instanceId(2), b.latestLayer)
}

func TestBroker_updateSynchronicity(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())
	b.isNodeSynced = trueFunc
	b.updateSynchronicity(1)
	r.True(b.isSynced(1))
	b.isNodeSynced = falseFunc
	b.updateSynchronicity(1)
	r.True(b.isSynced(1))
	b.updateSynchronicity(2)
	r.False(b.isSynced(2))
}

func TestBroker_isSynced(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())
	b.isNodeSynced = trueFunc
	r.True(b.isSynced(1))
	b.isNodeSynced = falseFunc
	r.True(b.isSynced(1))
	r.False(b.isSynced(2))
	b.isNodeSynced = trueFunc
	r.False(b.isSynced(2))
}

func TestBroker_Register4(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())
	b.Start()
	b.isNodeSynced = trueFunc
	c, e := b.Register(1)
	r.Nil(e)
	r.Equal(b.outbox[1], c)

	b.isNodeSynced = falseFunc
	_, e = b.Register(2)
	r.NotNil(e)
}

func TestBroker_eventLoop(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())
	b.Start()

	// unknown-->invalid, ignore
	b.isNodeSynced = falseFunc
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewSetFromValues(value1)).Message
	m.InnerMsg.InstanceId = instanceId1
	msg := newMockGossipMsg(m)
	b.inbox <- msg
	_, ok := b.outbox[instanceId1]
	r.False(ok)

	// register to invalid should error
	_, e := b.Register(instanceId1)
	r.NotNil(e)

	// register to unknown->valid
	b.isNodeSynced = trueFunc
	c, e := b.Register(instanceId2)
	r.Nil(e)
	m.InnerMsg.InstanceId = instanceId2
	msg = newMockGossipMsg(m)
	b.inbox <- msg
	recM := <-c
	r.Equal(msg.Bytes(), recM.Bytes())

	// unknown->valid early
	m.InnerMsg.InstanceId = instanceId3
	msg = newMockGossipMsg(m)
	b.inbox <- msg
	c, e = b.Register(instanceId3)
	r.Nil(e)
	<-c
}

func TestBroker_eventLoop2(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())
	b.Start()

	// invalid instance
	b.isNodeSynced = falseFunc
	_, e := b.Register(instanceId4)
	r.NotNil(e)
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewSetFromValues(value1)).Message
	m.InnerMsg.InstanceId = instanceId4
	msg := newMockGossipMsg(m)
	b.inbox <- msg
	v, ok := b.syncState[instanceId4]
	r.True(ok)
	r.NotEqual(true, v)

	// valid but not early
	m.InnerMsg.InstanceId = instanceId6
	msg = newMockGossipMsg(m)
	b.inbox <- msg
	_, ok = b.outbox[instanceId6]
	r.False(ok)
}

func Test_validate(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())

	m := BuildStatusMsg(signing.NewEdSigner(), NewDefaultEmptySet())
	m.InnerMsg.InstanceId = 1
	b.latestLayer = 2
	e := b.validate(m.Message)
	r.EqualError(e, errUnregistered.Error())

	m.InnerMsg.InstanceId = 2
	e = b.validate(m.Message)
	r.EqualError(e, errRegistration.Error())

	m.InnerMsg.InstanceId = 3
	e = b.validate(m.Message)
	r.EqualError(e, errEarlyMsg.Error())

	m.InnerMsg.InstanceId = 2
	b.outbox[2] = make(chan *Msg)
	b.syncState[2] = false
	e = b.validate(m.Message)
	r.EqualError(e, errNotSynced.Error())

	b.syncState[2] = true
	e = b.validate(m.Message)
	r.Nil(e)
}

func TestBroker_clean(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())

	for i := instanceId(1); i < 10; i++ {
		b.syncState[i] = true
	}

	b.latestLayer = 9
	b.outbox[5] = make(chan *Msg)
	b.cleanOldLayers()
	r.Equal(instanceId(4), b.minDeleted)
	r.Equal(5, len(b.syncState))

	delete(b.outbox, 5)
	b.cleanOldLayers()
	r.Equal(1, len(b.syncState))
}

func TestBroker_Flow(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())

	b.Start()

	m := BuildStatusMsg(signing.NewEdSigner(), NewDefaultEmptySet())
	m.InnerMsg.InstanceId = 1
	b.inbox <- newMockGossipMsg(m.Message)
	ch1, e := b.Register(1)
	r.Nil(e)
	<-ch1

	m2 := BuildStatusMsg(signing.NewEdSigner(), NewDefaultEmptySet())
	m2.InnerMsg.InstanceId = 2
	ch2, e := b.Register(2)
	r.Nil(e)

	b.inbox <- newMockGossipMsg(m.Message)
	b.inbox <- newMockGossipMsg(m2.Message)

	<-ch2
	<-ch1

	b.Register(3)
	b.Register(4)
	b.Unregister(2)
	r.Equal(instanceId0, b.minDeleted)

	// check still receiving msgs on ch1
	b.inbox <- newMockGossipMsg(m.Message)
	<-ch1

	b.Unregister(1)
	r.Equal(instanceId2, b.minDeleted)
}
