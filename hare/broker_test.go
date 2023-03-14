package hare

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-scale"
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
	broker.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any())
	broker.Start(context.Background())
	t.Cleanup(broker.Close)

	lid := instanceID1
	inbox, err := broker.Register(context.Background(), lid)
	assert.Nil(t, err)

	serMsg := createMessage(t, lid)
	broker.HandleMessage(context.Background(), "", serMsg)
	waitForMessages(t, inbox, lid, 1)
}

// test that aborting the broker aborts.
func TestBroker_Abort(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
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

func waitForMessages(t *testing.T, inbox chan any, instanceID types.LayerID, msgCount int) {
	i := 0
	for {
		tm := time.NewTimer(3 * time.Second)
		for {
			select {
			case msg := <-inbox:
				x, ok := msg.(*Msg)
				require.True(t, ok)
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
	broker.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).AnyTimes()
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
	broker.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
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
	return &Msg{Message: msg}
}

func TestBroker_Send(t *testing.T) {
	ctx := context.Background()
	broker := buildBroker(t, t.Name())
	mev := &mockEligibilityValidator{valid: 0}
	broker.roleValidator = mev
	broker.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).AnyTimes()
	broker.Start(ctx)
	t.Cleanup(broker.Close)

	require.Equal(t, pubsub.ValidationIgnore, broker.HandleMessage(ctx, "", nil))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	msg := BuildPreRoundMsg(signer, NewSetFromValues(types.ProposalID{1}), nil).Message
	msg.Layer = instanceID2
	require.Equal(t, pubsub.ValidationIgnore, broker.HandleMessage(ctx, "", mustEncode(t, msg)))

	msg.Layer = instanceID1
	require.Equal(t, pubsub.ValidationIgnore, broker.HandleMessage(ctx, "", mustEncode(t, msg)))
	// nothing happens since this is an invalid InnerMsg

	atomic.StoreInt32(&mev.valid, 1)
	require.Equal(t, pubsub.ValidationAccept, broker.HandleMessage(ctx, "", mustEncode(t, msg)))
}

func TestBroker_HandleMaliciousHareMessage(t *testing.T) {
	ctx := context.Background()
	broker := buildBroker(t, t.Name())
	mch := make(chan *types.MalfeasanceGossip, 1)
	broker.mchOut = mch
	mev := &mockEligibilityValidator{valid: 1}
	broker.roleValidator = mev
	broker.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.Start(ctx)
	t.Cleanup(broker.Close)

	inbox, err := broker.Register(context.Background(), instanceID1)
	require.NoError(t, err)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, NewSetFromValues(types.ProposalID{1}), nil)
	data := mustEncode(t, m.Message)

	broker.mockMesh.EXPECT().GetMalfeasanceProof(signer.NodeID())
	require.Equal(t, pubsub.ValidationAccept, broker.HandleMessage(ctx, "", data))
	require.Len(t, inbox, 1)
	got := <-inbox
	msg, ok := got.(*Msg)
	require.True(t, ok)
	require.EqualValues(t, m, msg)

	proof := types.MalfeasanceProof{
		Layer: types.NewLayerID(1111),
		Proof: types.Proof{
			Type: types.MultipleBallots,
			Data: &types.BallotProof{
				Messages: [2]types.BallotProofMsg{{}, {}},
			},
		},
	}
	broker.mockMesh.EXPECT().GetMalfeasanceProof(signer.NodeID()).Return(&proof, nil)
	gossip := &types.MalfeasanceGossip{
		MalfeasanceProof: proof,
		Eligibility: &types.HareEligibilityGossip{
			Layer:       instanceID1,
			Round:       preRound,
			PubKey:      signer.PublicKey().Bytes(),
			Eligibility: msg.Eligibility,
		},
	}
	require.Equal(t, pubsub.ValidationIgnore, broker.HandleMessage(ctx, "", data))
	require.Len(t, mch, 1)
	gotG := <-mch
	require.EqualValues(t, gossip, gotG)
	require.Len(t, inbox, 1)
	got = <-inbox
	em, ok := got.(*types.HareEligibilityGossip)
	require.True(t, ok)
	require.EqualValues(t, gossip.Eligibility, em)
}

func TestBroker_HandleEligibility(t *testing.T) {
	ctx := context.Background()
	broker := buildBroker(t, t.Name())
	mev := &mockEligibilityValidator{valid: 0}
	broker.roleValidator = mev
	broker.Start(ctx)
	t.Cleanup(broker.Close)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	em := &types.HareEligibilityGossip{
		Layer:  instanceID1,
		Round:  preRound,
		PubKey: signer.PublicKey().Bytes(),
		Eligibility: types.HareEligibility{
			Proof: []byte{1, 2, 3},
			Count: 3,
		},
	}

	t.Run("consensus not running", func(t *testing.T) {
		require.False(t, broker.HandleEligibility(context.Background(), em))
	})

	var inbox chan any

	t.Run("identity active check failed", func(t *testing.T) {
		errUnknown := errors.New("blah")
		em.Layer = instanceID1
		inbox, err = broker.Register(context.Background(), instanceID1)
		require.NoError(t, err)
		require.NotNil(t, inbox)
		broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(false, errUnknown)
		require.False(t, broker.HandleEligibility(context.Background(), em))
		require.Len(t, inbox, 0)
	})

	t.Run("identity not active", func(t *testing.T) {
		inbox, err = broker.Register(context.Background(), instanceID2)
		em.Layer = instanceID2
		require.NoError(t, err)
		require.NotNil(t, inbox)
		broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)
		require.False(t, broker.HandleEligibility(context.Background(), em))
		require.Len(t, inbox, 0)
	})

	t.Run("identity not eligible", func(t *testing.T) {
		inbox, err = broker.Register(context.Background(), instanceID3)
		em.Layer = instanceID3
		require.NoError(t, err)
		require.NotNil(t, inbox)
		broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil)
		require.False(t, broker.HandleEligibility(context.Background(), em))
		require.Len(t, inbox, 0)
	})

	t.Run("identity eligible", func(t *testing.T) {
		inbox, err = broker.Register(context.Background(), instanceID4)
		em.Layer = instanceID4
		require.NoError(t, err)
		require.NotNil(t, inbox)
		broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil)
		atomic.StoreInt32(&mev.valid, 1)
		require.True(t, broker.HandleEligibility(context.Background(), em))
		require.Len(t, inbox, 1)
		msg := <-inbox
		got, ok := msg.(*types.HareEligibilityGossip)
		require.True(t, ok)
		require.EqualValues(t, em, got)
	})
}

func TestBroker_RegisterOldLayer(t *testing.T) {
	broker := buildBroker(t, t.Name())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	broker.Start(ctx)
	t.Cleanup(broker.Close)
	defer func() {
		assert.NotNil(t, recover())
	}()

	_, err := broker.Register(ctx, instanceID2)
	require.NoError(t, err)
	_, err = broker.Register(ctx, instanceID1)
	require.NoError(t, err)
}

func TestBroker_Register(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	broker.Start(ctx)
	t.Cleanup(broker.Close)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	// Registering for a new layer with no early messages should result in a 0
	// length channel.
	out, err := broker.Register(ctx, instanceID1)
	require.NoError(t, err)
	assert.Equal(t, 0, len(out))

	// Send an early message
	msg := BuildPreRoundMsg(signer, NewSetFromValues(types.ProposalID{1}), nil)
	msg.Layer = instanceID2
	buf := &bytes.Buffer{}
	_, err = msg.EncodeScale(scale.NewEncoder(buf))
	require.NoError(t, err)
	err = broker.handleMessage(ctx, buf.Bytes())
	require.NoError(t, err)

	// Registering for a with a messages should result in a 1
	// length channel.
	out, err = broker.Register(ctx, instanceID2)
	require.NoError(t, err)
	assert.Equal(t, 1, len(out))
}

func TestBroker_Register2(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).AnyTimes()
	broker.Start(context.Background())
	t.Cleanup(broker.Close)
	broker.Register(context.Background(), instanceID1)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, NewSetFromValues(types.ProposalID{1}), nil).Message
	m.Layer = instanceID1

	msg := newMockGossipMsg(m).Message
	require.Equal(t, pubsub.ValidationAccept, broker.HandleMessage(context.Background(), "", mustEncode(t, msg)))

	m.Layer = instanceID2
	msg = newMockGossipMsg(m).Message
	require.Equal(t, pubsub.ValidationAccept, broker.HandleMessage(context.Background(), "", mustEncode(t, msg)))
}

func TestBroker_Register3(t *testing.T) {
	broker := buildBroker(t, t.Name())
	broker.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any())
	broker.Start(context.Background())
	t.Cleanup(broker.Close)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, NewSetFromValues(types.ProposalID{1}), nil).Message
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
	broker.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	broker.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	broker.mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any())
	broker.Start(context.Background())
	t.Cleanup(broker.Close)
	inbox, _ := broker.Register(context.Background(), instanceID1)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, NewSetFromValues(types.ProposalID{1}), nil).Message
	m.Layer = instanceID1

	broker.HandleMessage(context.Background(), "", mustEncode(t, m))

	tm := time.NewTimer(2 * time.Second)
	for {
		select {
		case msg := <-inbox:
			inMsg, ok := msg.(*Msg)
			require.True(t, ok)
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
	m := BuildPreRoundMsg(signer, NewSetFromValues(types.ProposalID{1}), nil).Message
	// TODO: remove this comment when ready
	//_, e := newMsg(m, MockStateQuerier{false, errors.New("my err")})
	//assert.NotNil(t, e)
	ctrl := gomock.NewController(t)
	sq := mocks.NewMockstateQuerier(ctrl)
	sq.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(1)

	_, e := newMsg(context.Background(), logtest.New(t), signer.NodeID(), m, sq)
	assert.NoError(t, e)
}

func TestBroker_eventLoop(t *testing.T) {
	r := require.New(t)
	b := buildBroker(t, t.Name())
	b.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	b.mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).AnyTimes()
	b.Start(context.Background())
	t.Cleanup(b.Close)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildPreRoundMsg(signer, NewSetFromValues(types.ProposalID{1}), nil).Message

	// not synced
	m.Layer = instanceID1
	msg := newMockGossipMsg(m).Message
	b.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(false)
	r.Equal(pubsub.ValidationIgnore, b.HandleMessage(context.Background(), "", mustEncode(t, msg)))

	// synced
	b.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	c, e := b.Register(context.Background(), instanceID1)
	r.Nil(e)
	r.Equal(pubsub.ValidationAccept, b.HandleMessage(context.Background(), "", mustEncode(t, msg)))
	recM := <-c
	rec, ok := recM.(*Msg)
	r.True(ok)
	r.Equal(msg, rec.Message)

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
	rec, ok = recM.(*Msg)
	r.True(ok)
	r.Equal(msg, rec.Message)
}

func Test_validate(t *testing.T) {
	r := require.New(t)
	b := buildBroker(t, t.Name())

	b.mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).AnyTimes()
	b.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	b.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	b.Start(ctx)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	mGood := BuildStatusMsg(signer, NewDefaultEmptySet())
	mGood.Layer = instanceID2
	mLate := BuildStatusMsg(signer, NewDefaultEmptySet())
	mLate.Layer = instanceID1
	mEarly := BuildStatusMsg(signer, NewDefaultEmptySet())
	mEarly.Layer = instanceID3
	mFuture := BuildStatusMsg(signer, NewDefaultEmptySet())
	mFuture.Layer = instanceID4

	_, err = b.Register(ctx, mGood.Layer)
	r.NoError(err)

	buf := &bytes.Buffer{}
	_, err = mGood.EncodeScale(scale.NewEncoder(buf))
	r.NoError(err)
	err = b.handleMessage(ctx, buf.Bytes())
	r.NoError(err)

	buf.Reset()
	_, err = mEarly.EncodeScale(scale.NewEncoder(buf))
	r.NoError(err)
	err = b.handleMessage(ctx, buf.Bytes())
	r.NoError(err)

	buf.Reset()
	_, err = mLate.EncodeScale(scale.NewEncoder(buf))
	r.NoError(err)
	err = b.handleMessage(ctx, buf.Bytes())
	r.Error(err)

	buf.Reset()
	_, err = mFuture.EncodeScale(scale.NewEncoder(buf))
	r.NoError(err)
	err = b.handleMessage(ctx, buf.Bytes())
	r.Error(err)
}

func TestBroker_Flow(t *testing.T) {
	r := require.New(t)
	b := buildBroker(t, t.Name())

	b.mockStateQ.EXPECT().IsIdentityActiveOnConsensusView(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	b.mockMesh.EXPECT().GetMalfeasanceProof(gomock.Any()).AnyTimes()
	b.mockSyncS.EXPECT().IsSyncedAtEpoch(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
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
	r.Nil(b.outbox[instanceID2.Uint32()])

	// check still receiving msgs on ch1
	b.HandleMessage(context.Background(), "", mustEncode(t, m.Message))
	<-ch1

	b.Unregister(context.Background(), instanceID1)
	r.Nil(b.outbox[instanceID1.Uint32()])
}
