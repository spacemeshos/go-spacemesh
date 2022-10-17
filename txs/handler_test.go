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
		has                      bool
		hasErr, parseErr, addErr error
		failed                   bool
	}{
		{
			desc: "Success",
		},
		{
			desc:     "Success raw",
			parseErr: errors.New("test"),
		},
		{
			desc: "Dup",
			has:  true,
		},
		{
			desc:   "HasFailed",
			hasErr: errors.New("test"),
			failed: true,
		},
		{
			desc:   "AddFailed",
			addErr: errors.New("test"),
			failed: true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			cstate := mocks.NewMockconservativeState(ctrl)
			th := NewTxHandler(cstate, logtest.New(t))

			signer := signing.NewEdSigner()
			tx := newTx(t, 3, 10, 1, signer)
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
			err := th.HandleBlockTransaction(context.TODO(), tx.Raw)
			if tc.failed {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func gossipExpectations(t *testing.T, hasErr, parseErr, addErr error, has, verify, noheader bool) (*TxHandler, *types.Transaction) {
	ctrl := gomock.NewController(t)
	cstate := mocks.NewMockconservativeState(ctrl)
	th := NewTxHandler(cstate, logtest.New(t))

	signer := signing.NewEdSigner()
	tx := newTx(t, 3, 10, 1, signer)
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
		if parseErr == nil {
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
		has                      bool
		noheader                 bool
		hasErr, addErr, parseErr error
		verify                   bool
		expect                   pubsub.ValidationResult
	}{
		{
			desc:   "Success",
			verify: true,
			expect: pubsub.ValidationAccept,
		},
		{
			desc:     "SuccessNoHeader",
			noheader: true,
			verify:   true,
			expect:   pubsub.ValidationAccept,
		},
		{
			desc:   "Dup",
			has:    true,
			expect: pubsub.ValidationIgnore,
		},
		{
			desc:   "HasFailed",
			hasErr: errors.New("test"),
			expect: pubsub.ValidationIgnore,
		},
		{
			desc:   "ParseFailed",
			addErr: errors.New("test"),
			expect: pubsub.ValidationIgnore,
		},
		{
			desc:   "VerifyFalse",
			expect: pubsub.ValidationIgnore,
		},
		{
			desc:   "AddFailed",
			verify: true,
			addErr: errors.New("test"),
			expect: pubsub.ValidationIgnore,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			th, tx := gossipExpectations(t,
				tc.hasErr, tc.parseErr, tc.addErr,
				tc.has, tc.verify, tc.noheader,
			)
			require.Equal(t,
				tc.expect,
				th.HandleGossipTransaction(context.TODO(), "peer", tx.Raw),
			)
		})
	}
}

func Test_HandleProposal(t *testing.T) {
	for _, tc := range []struct {
		desc                     string
		has                      bool
		noheader                 bool
		hasErr, addErr, parseErr error
		verify                   bool
		fail                     bool
	}{
		{
			desc:   "Success",
			verify: true,
		},
		{
			desc:     "SuccessNoHeader",
			noheader: true,
			verify:   true,
		},
		{
			desc: "Dup",
			has:  true,
		},
		{
			desc:   "HasFailed",
			hasErr: errors.New("test"),
			fail:   true,
		},
		{
			desc:     "ParseFailed",
			parseErr: errors.New("test"),
			fail:     true,
		},
		{
			desc: "VerifyFalse",
			fail: true,
		},
		{
			desc:   "AddFailed",
			verify: true,
			addErr: errors.New("test"),
			fail:   true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			th, tx := gossipExpectations(t,
				tc.hasErr, tc.parseErr, tc.addErr,
				tc.has, tc.verify, tc.noheader,
			)
			err := th.HandleProposalTransaction(context.TODO(), tx.Raw)
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
