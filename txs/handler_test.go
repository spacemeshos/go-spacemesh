package txs

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/txs/mocks"
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
			cstate := mocks.NewMockconservativeState(ctrl)
			th := NewTxHandler(cstate, logtest.New(t))

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
			err = th.HandleBlockTransaction(context.Background(), tx.Raw)
			if tc.failed {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func gossipExpectations(t *testing.T, fee uint64, hasErr, parseErr, addErr error, has, verify, noheader bool) (*TxHandler, *types.Transaction) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tx := newTx(t, 3, 10, fee, signer)
	var rst *types.MeshTransaction
	if has {
		rst = &types.MeshTransaction{Transaction: *tx}
		if noheader {
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

func Test_HandleGossip(t *testing.T) {
	for _, tc := range []struct {
		desc                     string
		fee                      uint64
		has                      bool
		noheader                 bool
		hasErr, addErr, parseErr error
		verify                   bool
		expect                   pubsub.ValidationResult
	}{
		{
			desc:   "Success",
			fee:    1,
			verify: true,
			expect: pubsub.ValidationAccept,
		},
		{
			desc:     "SuccessNoHeader",
			fee:      1,
			noheader: true,
			verify:   true,
			expect:   pubsub.ValidationAccept,
		},
		{
			desc:   "Dup",
			fee:    1,
			has:    true,
			expect: pubsub.ValidationIgnore,
		},
		{
			desc:   "HasFailed",
			fee:    1,
			hasErr: errors.New("test"),
			expect: pubsub.ValidationIgnore,
		},
		{
			desc:   "ParseFailed",
			fee:    1,
			addErr: errors.New("test"),
			expect: pubsub.ValidationIgnore,
		},
		{
			desc:   "VerifyFalse",
			fee:    1,
			expect: pubsub.ValidationIgnore,
		},
		{
			desc:   "AddFailed",
			fee:    1,
			verify: true,
			addErr: errors.New("test"),
			expect: pubsub.ValidationIgnore,
		},
		{
			desc:   "ZeroPrice",
			fee:    1,
			expect: pubsub.ValidationIgnore,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			th, tx := gossipExpectations(t, tc.fee,
				tc.hasErr, tc.parseErr, tc.addErr,
				tc.has, tc.verify, tc.noheader,
			)
			require.Equal(t,
				tc.expect,
				th.HandleGossipTransaction(context.Background(), "peer", tx.Raw),
			)
		})
	}
}

func Test_HandleProposal(t *testing.T) {
	for _, tc := range []struct {
		desc                     string
		fee                      uint64
		has                      bool
		noheader                 bool
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
			noheader: true,
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
				tc.has, tc.verify, tc.noheader,
			)
			err := th.HandleProposalTransaction(context.Background(), tx.Raw)
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
