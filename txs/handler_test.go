package txs

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

func Test_HandleBlock(t *testing.T) {
	for _, tc := range []struct {
		desc                     string
		fee                      uint64
		has                      bool
		hasErr, parseErr, addErr error
		failed                   bool
	}{
		{
			desc: "Success",
			fee:  1,
		},
		{
			desc:     "Success raw",
			fee:      1,
			parseErr: errors.New("test"),
		},
		{
			desc: "Dup",
			fee:  1,
			has:  true,
		},
		{
			desc:   "HasFailed",
			fee:    1,
			hasErr: errors.New("test"),
			failed: true,
		},
		{
			desc:   "AddFailed",
			fee:    1,
			addErr: errors.New("test"),
			failed: true,
		},
		{
			desc: "ZeroPrice",
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			cstate := NewMockconservativeState(ctrl)
			_, pub, err := crypto.GenerateEd25519Key(nil)
			require.NoError(t, err)
			id, err := peer.IDFromPublicKey(pub)
			require.NoError(t, err)
			th := NewTxHandler(cstate, id, logtest.New(t))

			signer, err := signing.NewEdSigner()
			require.NoError(t, err)
			tx := newTx(t, 3, 10, tc.fee, signer)
			cstate.EXPECT().HasTx(tx.ID).Return(tc.has, tc.hasErr).Times(1)
			if tc.hasErr == nil && !tc.has {
				req := smocks.NewMockValidationRequest(ctrl)
				req.EXPECT().Parse().Times(1).Return(tx.TxHeader, tc.parseErr)
				cstate.EXPECT().Validation(tx.RawTx).Times(1).Return(req)
				if tc.parseErr == nil {
					req.EXPECT().Verify().Times(1).Return(true)
					cstate.EXPECT().AddToDB(&types.Transaction{RawTx: tx.RawTx, TxHeader: tx.TxHeader}).Return(tc.addErr)
				} else {
					cstate.EXPECT().AddToDB(&types.Transaction{RawTx: tx.RawTx}).Return(tc.addErr)
				}
			}
			err = th.HandleBlockTransaction(context.Background(), p2p.NoPeer, tx.Raw)
			if tc.failed {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func gossipExpectations(t *testing.T, fee uint64, hasErr, parseErr, addErr error, has, verify, noHeader bool) (*TxHandler, *types.Transaction) {
	ctrl := gomock.NewController(t)
	cstate := NewMockconservativeState(ctrl)
	_, pub, err := crypto.GenerateEd25519Key(nil)
	require.NoError(t, err)
	id, err := peer.IDFromPublicKey(pub)
	require.NoError(t, err)
	th := NewTxHandler(cstate, id, logtest.New(t))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tx := newTx(t, 3, 10, fee, signer)
	var rst *types.MeshTransaction
	if has {
		rst = &types.MeshTransaction{Transaction: *tx}
		if noHeader {
			rst.TxHeader = nil
		}
	}
	cstate.EXPECT().GetMeshTransaction(tx.ID).Return(rst, hasErr).Times(1)
	if hasErr == nil && !has {
		req := smocks.NewMockValidationRequest(ctrl)
		req.EXPECT().Parse().Times(1).Return(tx.TxHeader, parseErr)
		cstate.EXPECT().Validation(tx.RawTx).Times(1).Return(req)
		if parseErr == nil && fee != 0 {
			req.EXPECT().Verify().Times(1).Return(verify)
			if verify {
				cstate.EXPECT().AddToCache(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, got *types.Transaction) error {
						assert.Equal(t, tx.ID, got.ID) // causing ID to be calculated
						assert.Equal(t, tx, got)
						return addErr
					}).Times(1)
			}
		}
	}
	return th, tx
}

func nilErr(err error) bool {
	return err == nil
}

func isErr(err error) bool {
	return err != nil
}

func Test_HandleGossip(t *testing.T) {
	for _, tc := range []struct {
		desc                     string
		fee                      uint64
		has                      bool
		noHeader                 bool
		hasErr, addErr, parseErr error
		verify                   bool
		expect                   func(error) bool
	}{
		{
			desc:   "Success",
			fee:    1,
			verify: true,
			expect: nilErr,
		},
		{
			desc:     "SuccessNoHeader",
			fee:      1,
			noHeader: true,
			verify:   true,
			expect:   nilErr,
		},
		{
			desc:   "Dup",
			fee:    1,
			has:    true,
			expect: isErr,
		},
		{
			desc:   "HasFailed",
			fee:    1,
			hasErr: errors.New("test"),
			expect: isErr,
		},
		{
			desc:   "ParseFailed",
			fee:    1,
			addErr: errors.New("test"),
			expect: isErr,
		},
		{
			desc:   "VerifyFalse",
			fee:    1,
			expect: isErr,
		},
		{
			desc:   "AddFailed",
			fee:    1,
			verify: true,
			addErr: errors.New("test"),
			expect: isErr,
		},
		{
			desc:   "ZeroPrice",
			fee:    1,
			expect: isErr,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			th, tx := gossipExpectations(t, tc.fee,
				tc.hasErr, tc.parseErr, tc.addErr,
				tc.has, tc.verify, tc.noHeader,
			)

			require.True(t,
				tc.expect(th.HandleGossipTransaction(context.Background(), p2p.NoPeer, tx.Raw)),
			)
		})
	}
}

func Test_HandleOwnGossip(t *testing.T) {
	_, pub, err := crypto.GenerateEd25519Key(nil)
	require.NoError(t, err)
	id, err := peer.IDFromPublicKey(pub)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	cstate := NewMockconservativeState(ctrl) // no calls on conservative state but still returns accepted
	th := NewTxHandler(cstate, id, logtest.New(t))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tx := newTx(t, 3, 10, 1, signer)

	require.Equal(t,
		nil,
		th.HandleGossipTransaction(context.Background(), id, tx.Raw),
	)
}

func Test_HandleProposal(t *testing.T) {
	for _, tc := range []struct {
		desc                     string
		fee                      uint64
		has                      bool
		noHeader                 bool
		hasErr, addErr, parseErr error
		verify                   bool
		fail                     bool
	}{
		{
			desc:   "Success",
			fee:    1,
			verify: true,
		},
		{
			desc:     "SuccessNoHeader",
			fee:      1,
			noHeader: true,
			verify:   true,
		},
		{
			desc: "Dup",
			fee:  1,
			has:  true,
		},
		{
			desc:   "HasFailed",
			fee:    1,
			hasErr: errors.New("test"),
			fail:   true,
		},
		{
			desc:     "ParseFailed",
			fee:      1,
			parseErr: errors.New("test"),
			fail:     true,
		},
		{
			desc: "VerifyFalse",
			fee:  1,
			fail: true,
		},
		{
			desc:   "AddFailed",
			fee:    1,
			verify: true,
			addErr: errors.New("test"),
			fail:   true,
		},
		{
			desc: "ZeroPrice",
			fail: true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			th, tx := gossipExpectations(t, tc.fee,
				tc.hasErr, tc.parseErr, tc.addErr,
				tc.has, tc.verify, tc.noHeader,
			)
			err := th.HandleProposalTransaction(context.Background(), p2p.NoPeer, tx.Raw)
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
