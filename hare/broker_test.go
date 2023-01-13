package hare

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

var (
	instanceID0 types.LayerID
	instanceID1 types.LayerID
	instanceID2 types.LayerID
	instanceID3 types.LayerID
	instanceID4 types.LayerID
	instanceID5 types.LayerID
	instanceID6 types.LayerID
)

func mustEncode(tb testing.TB, msg Message) []byte {
	tb.Helper()
	buf, err := codec.Encode(&msg)
	require.NoError(tb, err)
	return buf
}

type mockClient struct {
	id types.LayerID
}

func createMessage(tb testing.TB, instanceID types.LayerID) []byte {
	tb.Helper()

	sr, err := signing.NewEdSigner()
	require.NoError(tb, err)
	b := newMessageBuilder()
	msg := b.SetPubKey(sr.PublicKey()).SetLayer(instanceID).Sign(sr).Build()
	return mustEncode(tb, msg.Message)
}

// test that a InnerMsg to a specific set ID is delivered by the broker.
func TestBroker_Received(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.Start(context.Background())
	t.Cleanup(broker.Close)

	inbox, err := broker.Register(context.Background(), instanceID1)
	assert.Nil(t, err)

	serMsg := createMessage(t, instanceID1)
	broker.HandleMessage(context.Background(), "", serMsg)
	waitForMessages(t, inbox, instanceID1, 1)
}

// test that after registering the maximum number of protocols,
// the earliest one gets unregistered in favor of the newest one.
func TestBroker_MaxConcurrentProcesses(t *testing.T) {
	broker := buildBrokerWithLimit(t, t.Name(), 4)
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.Start(context.Background())
	t.Cleanup(broker.Close)

	broker.Register(context.Background(), instanceID1)
	broker.Register(context.Background(), instanceID2)
	broker.Register(context.Background(), instanceID3)
	broker.Register(context.Background(), instanceID4)

	broker.mu.RLock()
	assert.Equal(t, 4, len(broker.outbox))
	broker.mu.RUnlock()

	// this statement should cause inbox1 to be unregistered
	inbox5, _ := broker.Register(context.Background(), instanceID5)
	broker.mu.RLock()
	assert.Equal(t, 4, len(broker.outbox))
	broker.mu.RUnlock()

	serMsg := createMessage(t, instanceID5)
	broker.HandleMessage(context.Background(), "", serMsg)
	waitForMessages(t, inbox5, instanceID5, 1)
	broker.mu.RLock()
	assert.Nil(t, broker.outbox[instanceID1.Uint32()])
	broker.mu.RUnlock()

	inbox6, _ := broker.Register(context.Background(), instanceID6)
	broker.mu.RLock()
	assert.Equal(t, 4, len(broker.outbox))
	broker.mu.RUnlock()
	broker.mu.RLock()
	assert.Nil(t, broker.outbox[instanceID2.Uint32()])
	broker.mu.RUnlock()

	serMsg = createMessage(t, instanceID6)
	broker.HandleMessage(context.Background(), "", serMsg)
	waitForMessages(t, inbox6, instanceID6, 1)
}

// test that aborting the broker aborts.
func TestBroker_Abort(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.Start(context.Background())

	timer := time.NewTimer(3 * time.Second)

	broker.Close()

	select {
	case <-broker.ctx.Done():
		assert.True(t, true)
	case <-timer.C:
		assert.Fail(t, "timeout")
	}
}

func sendMessages(t *testing.T, instanceID types.LayerID, broker *Broker, count int) {
	for i := 0; i < count; i++ {
		broker.HandleMessage(context.Background(), "", createMessage(t, instanceID))
	}
}

func waitForMessages(t *testing.T, inbox chan *Msg, instanceID types.LayerID, msgCount int) {
	i := 0
	for {
		tm := time.NewTimer(3 * time.Second)
		for {
			select {
			case x := <-inbox:
				assert.True(t, x.Layer == instanceID)
				i++
				if i >= msgCount {
					return
				}
			case <-tm.C:
				t.Errorf("Timed out waiting for msg %v", i)
				t.Fail()
				return
			}
		}
	}
}

// test flow for multiple set ObjectID.
func TestBroker_MultipleInstanceIds(t *testing.T) {
	const msgCount = 1

	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.Start(context.Background())
	t.Cleanup(broker.Close)

	inbox1, err := broker.Register(context.Background(), instanceID1)
	require.NoError(t, err)
	inbox2, err := broker.Register(context.Background(), instanceID2)
	require.NoError(t, err)
	inbox3, err := broker.Register(context.Background(), instanceID3)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		sendMessages(t, instanceID1, broker.Broker, msgCount)
	}()
	go func() {
		defer wg.Done()
		sendMessages(t, instanceID2, broker.Broker, msgCount)
	}()
	go func() {
		defer wg.Done()
		sendMessages(t, instanceID3, broker.Broker, msgCount)
	}()

	go func() {
		defer wg.Done()
		waitForMessages(t, inbox1, instanceID1, msgCount)
	}()
	go func() {
		defer wg.Done()
		waitForMessages(t, inbox2, instanceID2, msgCount)
	}()
	waitForMessages(t, inbox3, instanceID3, msgCount)

	wg.Wait()
}

func TestBroker_RegisterUnregister(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.Start(context.Background())
	t.Cleanup(broker.Close)

	broker.Register(context.Background(), instanceID1)

	broker.mu.RLock()
	assert.Equal(t, 1, len(broker.outbox))
	broker.mu.RUnlock()

	broker.Unregister(context.Background(), instanceID1)
	broker.mu.RLock()
	assert.Nil(t, broker.outbox[instanceID1.Uint32()])
	broker.mu.RUnlock()
}

func newMockGossipMsg(msg Message) *Msg {
	return &Msg{msg, nil, ""}
}

func TestBroker_Send(t *testing.T) {
	ctx := context.Background()
	broker := buildBroker(t, t.Name())
	mev := &mockEligibilityValidator{valid: 0}
	broker.eValidator = mev
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.Start(ctx)
	t.Cleanup(broker.Close)

	require.Equal(t, pubsub.ValidationIgnore, broker.HandleMessage(ctx, "", nil))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	msg := BuildPreRoundMsg(signer, NewSetFromValues(value1), nil).Message
	msg.Layer = instanceID2
	require.Equal(t, pubsub.ValidationIgnore, broker.HandleMessage(ctx, "", mustEncode(t, msg)))

	msg.Layer = instanceID1
	require.Equal(t, pubsub.ValidationIgnore, broker.HandleMessage(ctx, "", mustEncode(t, msg)))
	// nothing happens since this is an invalid InnerMsg

	atomic.StoreInt32(&mev.valid, 1)
	require.Equal(t, pubsub.ValidationAccept, broker.HandleMessage(ctx, "", mustEncode(t, msg)))
}

func TestBroker_Register(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.Start(context.Background())
	t.Cleanup(broker.Close)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	msg := BuildPreRoundMsg(signer, NewSetFromValues(value1), nil)

	broker.mu.Lock()
	broker.pending[instanceID1.Uint32()] = []*Msg{msg, msg}
	broker.mu.Unlock()

	broker.Register(context.Background(), instanceID1)

	broker.mu.RLock()
	assert.Equal(t, 2, len(broker.outbox[instanceID1.Uint32()]))
	assert.Equal(t, 0, len(broker.pending[instanceID1.Uint32()]))
	broker.mu.RUnlock()
}

func TestBroker_Register2(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.Start(context.Background())
	t.Cleanup(broker.Close)
	broker.Register(context.Background(), instanceID1)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, NewSetFromValues(value1), nil).Message
	m.Layer = instanceID1

	msg := newMockGossipMsg(m).Message
	require.Equal(t, pubsub.ValidationAccept, broker.HandleMessage(context.Background(), "", mustEncode(t, msg)))

	m.Layer = instanceID2
	msg = newMockGossipMsg(m).Message
	require.Equal(t, pubsub.ValidationAccept, broker.HandleMessage(context.Background(), "", mustEncode(t, msg)))
}

func TestBroker_Register3(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.Start(context.Background())
	t.Cleanup(broker.Close)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, NewSetFromValues(value1), nil).Message
	m.Layer = instanceID1

	broker.HandleMessage(context.Background(), "", mustEncode(t, m))
	time.Sleep(1 * time.Millisecond)
	client := mockClient{instanceID1}
	ch, _ := broker.Register(context.Background(), client.id)
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
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.Start(context.Background())
	t.Cleanup(broker.Close)
	inbox, _ := broker.Register(context.Background(), instanceID1)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, NewSetFromValues(value1), nil).Message
	m.Layer = instanceID1

	broker.HandleMessage(context.Background(), "", mustEncode(t, m))

	tm := time.NewTimer(2 * time.Second)
	for {
		select {
		case inMsg := <-inbox:
			assert.True(t, signer.PublicKey().Equals(inMsg.PubKey))
			return
		case <-tm.C:
			t.Error("Timeout")
			t.FailNow()
			return
		}
	}
}

func Test_newMsg(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, NewSetFromValues(value1), nil).Message
	// TODO: remove this comment when ready
	//_, e := newMsg(m, MockStateQuerier{false, errors.New("my err")})
	//assert.NotNil(t, e)
	ctrl := gomock.NewController(t)
	sq := mocks.NewMockstateQuerier(ctrl)
	sq.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(1)

	_, e := newMsg(context.Background(), logtest.New(t), m, sq)
	assert.NoError(t, e)
}

func TestBroker_updateInstance(t *testing.T) {
	r := require.New(t)

	b := buildBroker(t, t.Name())
	r.Equal(instanceID0, b.getLatestLayer())
	b.setLatestLayer(context.Background(), instanceID1)
	r.Equal(instanceID1, b.getLatestLayer())

	b.setLatestLayer(context.Background(), instanceID2)
	r.Equal(instanceID2, b.getLatestLayer())
}

func TestBroker_Synced(t *testing.T) {
	r := require.New(t)
	b := buildBroker(t, t.Name())
	b.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true)
	b.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true)
	r.True(b.Synced(context.Background(), instanceID1))

	b.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(false)
	r.False(b.Synced(context.Background(), instanceID1))

	b.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true)
	b.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(false)
	r.False(b.Synced(context.Background(), instanceID1))
}

func TestBroker_Register4(t *testing.T) {
	r := require.New(t)
	b := buildBroker(t, t.Name())
	b.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true)
	b.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true)
	b.Start(context.Background())
	t.Cleanup(b.Close)

	c, e := b.Register(context.Background(), instanceID1)
	r.NoError(e)

	b.mu.RLock()
	r.Equal(b.outbox[instanceID1.Value], c)
	b.mu.RUnlock()

	b.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(false)
	_, e = b.Register(context.Background(), instanceID2)
	r.NotNil(e)
}

func TestBroker_eventLoop(t *testing.T) {
	r := require.New(t)
	b := buildBroker(t, t.Name())
	b.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	b.Start(context.Background())
	t.Cleanup(b.Close)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, NewSetFromValues(value1), nil).Message

	// not synced
	m.Layer = instanceID1
	msg := newMockGossipMsg(m).Message
	b.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(false)
	r.Equal(pubsub.ValidationIgnore, b.HandleMessage(context.Background(), "", mustEncode(t, msg)))

	b.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(false)
	_, e := b.Register(context.Background(), instanceID1)
	r.NotNil(e)

	// synced
	b.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	b.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	c, e := b.Register(context.Background(), instanceID1)
	r.Nil(e)
	r.Equal(pubsub.ValidationAccept, b.HandleMessage(context.Background(), "", mustEncode(t, msg)))
	recM := <-c
	r.Equal(msg, recM.Message)

	// early message
	m.Layer = instanceID2
	msg = newMockGossipMsg(m).Message
	r.Equal(pubsub.ValidationAccept, b.HandleMessage(context.Background(), "", mustEncode(t, msg)))

	// future message
	m.Layer = instanceID3
	msg = newMockGossipMsg(m).Message
	r.Equal(pubsub.ValidationIgnore, b.HandleMessage(context.Background(), "", mustEncode(t, msg)))

	c, e = b.Register(context.Background(), instanceID3)
	r.Nil(e)
	r.Equal(pubsub.ValidationAccept, b.HandleMessage(context.Background(), "", mustEncode(t, msg)))
	recM = <-c
	r.Equal(msg, recM.Message)
}

func Test_validate(t *testing.T) {
	r := require.New(t)
	b := buildBroker(t, t.Name())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildStatusMsg(signer, NewDefaultEmptySet())
	m.Layer = instanceID1
	b.setLatestLayer(context.Background(), instanceID2)
	e := b.validate(context.Background(), &m.Message)
	r.ErrorIs(e, errUnregistered)

	m.Layer = instanceID2
	e = b.validate(context.Background(), &m.Message)
	r.ErrorIs(e, errRegistration)

	m.Layer = instanceID3
	e = b.validate(context.Background(), &m.Message)
	r.ErrorIs(e, errEarlyMsg)

	m.Layer = instanceID4
	e = b.validate(context.Background(), &m.Message)
	r.ErrorIs(e, errFutureMsg)
}

func TestBroker_clean(t *testing.T) {
	r := require.New(t)
	b := buildBroker(t, t.Name())

	ten := instanceID0.Add(10)
	b.setLatestLayer(context.Background(), ten.Sub(1))

	b.mu.Lock()
	b.outbox[5] = make(chan *Msg)
	b.mu.Unlock()

	b.cleanOldLayers()
	r.Equal(ten.Sub(2), b.minDeleted)

	b.mu.Lock()
	delete(b.outbox, 5)
	b.mu.Unlock()

	b.cleanOldLayers()
}

func TestBroker_Flow(t *testing.T) {
	r := require.New(t)
	b := buildBroker(t, t.Name())

	b.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	b.mockSyncS.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	b.mockSyncS.EXPECT().IsBeaconSynced(gomock.Any()).Return(true).AnyTimes()
	b.Start(context.Background())
	t.Cleanup(b.Close)

	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildStatusMsg(signer1, NewDefaultEmptySet())
	m.Layer = instanceID1
	b.HandleMessage(context.Background(), "", mustEncode(t, m.Message))

	ch1, e := b.Register(context.Background(), instanceID1)
	r.Nil(e)
	<-ch1

	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	m2 := BuildStatusMsg(signer2, NewDefaultEmptySet())
	m2.Layer = instanceID2
	ch2, e := b.Register(context.Background(), instanceID2)
	r.Nil(e)

	b.HandleMessage(context.Background(), "", mustEncode(t, m.Message))

	b.HandleMessage(context.Background(), "", mustEncode(t, m2.Message))

	<-ch2
	<-ch1

	b.Register(context.Background(), instanceID3)
	b.Register(context.Background(), instanceID4)
	b.Unregister(context.Background(), instanceID2)
	r.Equal(instanceID0, b.minDeleted)

	// check still receiving msgs on ch1
	b.HandleMessage(context.Background(), "", mustEncode(t, m.Message))
	<-ch1

	b.Unregister(context.Background(), instanceID1)
	r.Equal(instanceID2, b.minDeleted)
}
