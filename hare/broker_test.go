package hare

import (
	"bytes"
	"sync"
	"testing"
	"time"

	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var instanceID0 = instanceID(0)
var instanceID1 = instanceID(1)
var instanceID2 = instanceID(2)
var instanceID3 = instanceID(3)
var instanceID4 = instanceID(4)
var instanceID5 = instanceID(5)
var instanceID6 = instanceID(6)
var instanceID7 = instanceID(7)

func trueFunc() bool {
	return true
}

func falseFunc() bool {
	return false
}

type mockClient struct {
	id instanceID
}

type MockStateQuerier struct {
	res bool
	err error
}

func NewMockStateQuerier() MockStateQuerier {
	return MockStateQuerier{true, nil}
}

func (msq MockStateQuerier) IsIdentityActiveOnConsensusView(edID string, layer types.LayerID) (bool, error) {
	return msq.res, msq.err
}

func createMessage(t *testing.T, instanceID instanceID) []byte {
	sr := signing.NewEdSigner()
	b := newMessageBuilder()
	msg := b.SetPubKey(sr.PublicKey()).SetInstanceID(instanceID).Sign(sr).Build()

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

// test that a InnerMsg to a specific set ID is delivered by the broker
func TestBroker_Received(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	broker := buildBroker(n1, t.Name())
	broker.Start()

	inbox, err := broker.Register(instanceID1)
	assert.Nil(t, err)

	serMsg := createMessage(t, instanceID1)
	n2.Broadcast(protoName, serMsg)
	waitForMessages(t, inbox, instanceID1, 1)
}

//test that after registering the maximum number of protocols,
//the earliest one gets unregistered in favor of the newest one
func TestBroker_MaxConcurrentProcesses(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	broker := buildBrokerLimit4(n1, t.Name())
	broker.Start()

	broker.Register(instanceID1)
	broker.Register(instanceID2)
	broker.Register(instanceID3)
	broker.Register(instanceID4)
	assert.Equal(t, 4, len(broker.outbox))

	//this statement should cause inbox1 to be unregistered
	inbox5, _ := broker.Register(instanceID5)
	assert.Equal(t, 4, len(broker.outbox))

	serMsg := createMessage(t, instanceID5)
	n2.Broadcast(protoName, serMsg)
	waitForMessages(t, inbox5, instanceID5, 1)
	assert.Nil(t, broker.outbox[instanceID1])

	inbox6, _ := broker.Register(instanceID6)
	assert.Equal(t, 4, len(broker.outbox))
	assert.Nil(t, broker.outbox[instanceID2])

	serMsg = createMessage(t, instanceID6)
	n2.Broadcast(protoName, serMsg)
	waitForMessages(t, inbox6, instanceID6, 1)
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

func sendMessages(t *testing.T, instanceID instanceID, n *service.Node, count int) {
	for i := 0; i < count; i++ {
		n.Broadcast(protoName, createMessage(t, instanceID))
	}
}

func waitForMessages(t *testing.T, inbox chan *Msg, instanceID instanceID, msgCount int) {
	i := 0
	for {
		tm := time.NewTimer(3 * time.Second)
		for {
			select {
			case x := <-inbox:
				assert.True(t, x.InnerMsg.InstanceID == instanceID)
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

// test flow for multiple set ObjectID
func TestBroker_MultipleInstanceIds(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	const msgCount = 1

	broker := buildBroker(n1, t.Name())
	broker.Start()

	inbox1, _ := broker.Register(instanceID1)
	inbox2, _ := broker.Register(instanceID2)
	inbox3, _ := broker.Register(instanceID3)

	go sendMessages(t, instanceID1, n2, msgCount)
	go sendMessages(t, instanceID2, n2, msgCount)
	go sendMessages(t, instanceID3, n2, msgCount)

	go waitForMessages(t, inbox1, instanceID1, msgCount)
	go waitForMessages(t, inbox2, instanceID2, msgCount)
	waitForMessages(t, inbox3, instanceID3, msgCount)

	assert.True(t, true)
}

func TestBroker_RegisterUnregister(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	broker := buildBroker(n1, t.Name())
	broker.Start()
	broker.Register(instanceID1)
	assert.Equal(t, 1, len(broker.outbox))
	broker.Unregister(instanceID1)
	assert.Nil(t, broker.outbox[instanceID1])
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
	msg.InnerMsg.InstanceID = 2
	m = newMockGossipMsg(msg)
	broker.inbox <- m

	msg.InnerMsg.InstanceID = 1
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
	broker.pending[instanceID1] = []*Msg{msg, msg}
	broker.Register(instanceID1)
	assert.Equal(t, 2, len(broker.outbox[instanceID1]))
	assert.Equal(t, 0, len(broker.pending[instanceID1]))
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
	broker.Register(instanceID1)
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewSetFromValues(value1)).Message
	m.InnerMsg.InstanceID = instanceID1
	msg := newMockGossipMsg(m)
	broker.inbox <- msg
	assertMsg(t, msg)
	m.InnerMsg.InstanceID = instanceID2
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
	m.InnerMsg.InstanceID = instanceID1
	msg := newMockGossipMsg(m)
	broker.inbox <- msg
	time.Sleep(1)
	client := mockClient{instanceID1}
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
	inbox, _ := broker.Register(instanceID1)
	sgn := signing.NewEdSigner()
	m := BuildPreRoundMsg(sgn, NewSetFromValues(value1)).Message
	m.InnerMsg.InstanceID = instanceID1
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
	_, e := newMsg(m, MockStateQuerier{true, nil})
	assert.Nil(t, e)
}

func TestBroker_updateInstance(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())
	r.Equal(instanceID(0), b.latestLayer)
	b.updateLatestLayer(1)
	r.Equal(instanceID(1), b.latestLayer)
	b.updateLatestLayer(2)
	r.Equal(instanceID(2), b.latestLayer)
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
	m.InnerMsg.InstanceID = instanceID1
	msg := newMockGossipMsg(m)
	b.inbox <- msg
	_, ok := b.outbox[instanceID1]
	r.False(ok)

	// register to invalid should error
	_, e := b.Register(instanceID1)
	r.NotNil(e)

	// register to unknown->valid
	b.isNodeSynced = trueFunc
	c, e := b.Register(instanceID2)
	r.Nil(e)
	m.InnerMsg.InstanceID = instanceID2
	msg = newMockGossipMsg(m)
	b.inbox <- msg
	recM := <-c
	r.Equal(msg.Bytes(), recM.Bytes())

	// unknown->valid early
	m.InnerMsg.InstanceID = instanceID3
	msg = newMockGossipMsg(m)
	b.inbox <- msg
	c, e = b.Register(instanceID3)
	r.Nil(e)
	<-c
}

func TestBroker_eventLoop2(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())
	b.Start()

	// invalid instance
	b.isNodeSynced = falseFunc
	_, e := b.Register(instanceID4)
	r.NotNil(e)
	m := BuildPreRoundMsg(signing.NewEdSigner(), NewSetFromValues(value1)).Message
	m.InnerMsg.InstanceID = instanceID4
	msg := newMockGossipMsg(m)
	b.inbox <- msg
	v, ok := b.syncState[instanceID4]
	r.True(ok)
	r.NotEqual(true, v)

	// valid but not early
	m.InnerMsg.InstanceID = instanceID6
	msg = newMockGossipMsg(m)
	b.inbox <- msg
	_, ok = b.outbox[instanceID6]
	r.False(ok)
}

func Test_validate(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())

	m := BuildStatusMsg(signing.NewEdSigner(), NewDefaultEmptySet())
	m.InnerMsg.InstanceID = 1
	b.latestLayer = 2
	e := b.validate(m.Message)
	r.EqualError(e, errUnregistered.Error())

	m.InnerMsg.InstanceID = 2
	e = b.validate(m.Message)
	r.EqualError(e, errRegistration.Error())

	m.InnerMsg.InstanceID = 3
	e = b.validate(m.Message)
	r.EqualError(e, errEarlyMsg.Error())

	m.InnerMsg.InstanceID = 2
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

	for i := instanceID(1); i < 10; i++ {
		b.syncState[i] = true
	}

	b.latestLayer = 9
	b.outbox[5] = make(chan *Msg)
	b.cleanOldLayers()
	r.Equal(instanceID(4), b.minDeleted)
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
	m.InnerMsg.InstanceID = 1
	b.inbox <- newMockGossipMsg(m.Message)
	ch1, e := b.Register(1)
	r.Nil(e)
	<-ch1

	m2 := BuildStatusMsg(signing.NewEdSigner(), NewDefaultEmptySet())
	m2.InnerMsg.InstanceID = 2
	ch2, e := b.Register(2)
	r.Nil(e)

	b.inbox <- newMockGossipMsg(m.Message)
	b.inbox <- newMockGossipMsg(m2.Message)

	<-ch2
	<-ch1

	b.Register(3)
	b.Register(4)
	b.Unregister(2)
	r.Equal(instanceID0, b.minDeleted)

	// check still receiving msgs on ch1
	b.inbox <- newMockGossipMsg(m.Message)
	<-ch1

	b.Unregister(1)
	r.Equal(instanceID2, b.minDeleted)
}

func TestBroker_Synced(t *testing.T) {
	r := require.New(t)
	b := buildBroker(service.NewSimulator().NewNode(), t.Name())
	b.Start()
	wg := sync.WaitGroup{}
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(j int) {
			r.True(b.Synced(instanceID(j)))
			wg.Done()
		}(i)
	}

	wg.Wait()
}
