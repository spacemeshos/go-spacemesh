package txs

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/txs/mocks"
)

func Test_HandleGossipTransaction_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	tx := newTx(t, 3, 10, 1, signer)
	origin := types.GenerateAddress(signer.PublicKey().Bytes())
	cstate.EXPECT().HasTx(tx.ID()).Return(false, nil).Times(1)
	cstate.EXPECT().AddressExists(origin).Return(true, nil).Times(1)
	cstate.EXPECT().AddToCache(gomock.Any(), true).DoAndReturn(
		func(got *types.Transaction, check bool) error {
			assert.Equal(t, tx.ID(), got.ID()) // causing ID to be calculated
			assert.Equal(t, tx, got)
			return nil
		}).Times(1)

	msg, err := codec.Encode(tx)
	require.NoError(t, err)

	got := th.HandleGossipTransaction(context.TODO(), "peer", msg)
	assert.Equal(t, pubsub.ValidationAccept, got)
}

func Test_HandleGossipTransaction_MalformedMsg(t *testing.T) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	msg, err := codec.Encode(newTx(t, 3, 10, 1, signer))
	require.NoError(t, err)

	got := th.HandleGossipTransaction(context.TODO(), "peer", msg[1:])
	assert.Equal(t, pubsub.ValidationIgnore, got)
}

func Test_handleTransaction_MalformedMsg(t *testing.T) {
	ctrl := gomock.NewController(t)
	th := NewTxHandler(mocks.NewMockconservativeState(ctrl), logtest.New(t))

	signer := signing.NewEdSigner()
	msg, err := codec.Encode(newTx(t, 3, 10, 1, signer))
	require.NoError(t, err)

	got := th.handleTransaction(context.TODO(), msg[1:])
	assert.ErrorIs(t, got, errMalformedMsg)
}

func Test_handleTransaction_BadSignature(t *testing.T) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	tx := newTx(t, 3, 10, 1, signer)
	cstate.EXPECT().HasTx(gomock.Any()).Return(false, nil).Times(1)
	tx.Signature[63] = 224
	msg, err := codec.Encode(tx)
	require.NoError(t, err)

	got := th.handleTransaction(context.TODO(), msg)
	assert.ErrorIs(t, got, errAddrNotExtracted)
}

func Test_handleTransaction_HasTXError(t *testing.T) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	tx := newTx(t, 3, 10, 1, signer)
	msg, err := codec.Encode(tx)
	require.NoError(t, err)

	errUnknown := errors.New("unknown")
	cstate.EXPECT().HasTx(tx.ID()).Return(false, errUnknown).Times(1)
	got := th.handleTransaction(context.TODO(), msg)
	assert.ErrorIs(t, got, errUnknown)
}

func Test_handleTransaction_DuplicateTX(t *testing.T) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	tx := newTx(t, 3, 10, 1, signer)
	msg, err := codec.Encode(tx)
	require.NoError(t, err)

	cstate.EXPECT().HasTx(tx.ID()).Return(true, nil).Times(1)
	got := th.handleTransaction(context.TODO(), msg)
	assert.ErrorIs(t, got, errDuplicateTX)
}

func Test_handleTransaction_AddressNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	tx := newTx(t, 3, 10, 1, signer)
	cstate.EXPECT().HasTx(tx.ID()).Return(false, nil).Times(1)
	origin := types.GenerateAddress(signer.PublicKey().Bytes())
	cstate.EXPECT().AddressExists(origin).Return(false, nil).Times(1)
	msg, err := codec.Encode(tx)
	require.NoError(t, err)

	got := th.handleTransaction(context.TODO(), msg)
	assert.ErrorIs(t, got, errAddrNotFound)
}

func Test_handleTransaction_FailedMemPoolIgnored(t *testing.T) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	tx := newTx(t, 3, 10, 1, signer)
	cstate.EXPECT().HasTx(tx.ID()).Return(false, nil).Times(1)
	origin := types.GenerateAddress(signer.PublicKey().Bytes())
	cstate.EXPECT().AddressExists(origin).Return(true, nil).Times(1)
	errUnknown := errors.New("unknown")
	cstate.EXPECT().AddToCache(gomock.Any(), true).DoAndReturn(
		func(got *types.Transaction, check bool) error {
			assert.Equal(t, tx.ID(), got.ID()) // causing ID to be calculated
			assert.Equal(t, tx, got)
			return errUnknown
		}).Times(1)

	msg, err := codec.Encode(tx)
	require.NoError(t, err)

	require.NoError(t, th.handleTransaction(context.TODO(), msg))
}

func Test_HandleSyncTransaction_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	tx := newTx(t, 3, 10, 1, signer)
	cstate.EXPECT().HasTx(tx.ID()).Return(false, nil).Times(1)
	cstate.EXPECT().AddToCache(gomock.Any(), true).DoAndReturn(
		func(got *types.Transaction, newTX bool) error {
			assert.Equal(t, tx.ID(), got.ID()) // causing ID to be calculated
			assert.Equal(t, tx, got)
			return nil
		}).Times(1)

	msg, err := codec.Encode(tx)
	require.NoError(t, err)

	got := th.HandleSyncTransaction(context.TODO(), msg, p2p.AnyPeer)
	assert.NoError(t, got)
}

func Test_HandleSyncTransaction_BadSignature(t *testing.T) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	tx := newTx(t, 3, 10, 1, signer)
	tx.Signature[63] = 224
	msg, err := codec.Encode(tx)
	require.NoError(t, err)

	got := th.HandleSyncTransaction(context.TODO(), msg, p2p.AnyPeer)
	assert.ErrorIs(t, got, errAddrNotExtracted)
}

func Test_HandleSyncTransaction_HasTXError(t *testing.T) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	tx := newTx(t, 3, 10, 1, signer)
	errUnknown := errors.New("unknown")
	cstate.EXPECT().HasTx(tx.ID()).Return(false, errUnknown).Times(1)
	msg, err := codec.Encode(tx)
	require.NoError(t, err)

	got := th.HandleSyncTransaction(context.TODO(), msg, p2p.AnyPeer)
	assert.ErrorIs(t, got, errUnknown)
}

func Test_HandleSyncTransaction_FailedMemPoolIgnored(t *testing.T) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	tx := newTx(t, 3, 10, 1, signer)
	errUnknown := errors.New("unknown")
	cstate.EXPECT().HasTx(tx.ID()).Return(false, nil).Times(1)
	cstate.EXPECT().AddToCache(gomock.Any(), true).DoAndReturn(
		func(got *types.Transaction, newTX bool) error {
			assert.Equal(t, tx.ID(), got.ID()) // causing ID to be calculated
			assert.Equal(t, tx, got)
			return errUnknown
		}).Times(1)

	msg, err := codec.Encode(tx)
	require.NoError(t, err)

	require.NoError(t, th.HandleSyncTransaction(context.TODO(), msg, p2p.AnyPeer))
}
