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

func Test_HandleSync(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		has            bool
		hasErr, addErr error
		failed         bool
	}{
		{
			desc: "Success",
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
				cstate.EXPECT().Add(&types.Transaction{RawTx: tx.RawTx}, gomock.Any()).
					Times(1).Return(tc.addErr)
			}
			err := th.HandleSyncTransaction(context.TODO(), tx.Raw)
			if tc.failed {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_HandleGossip(t *testing.T) {
	for _, tc := range []struct {
		desc                     string
		has                      bool
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
			expect: pubsub.ValidationAccept,
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
					req.EXPECT().Verify().Times(1).Return(tc.verify)
					if tc.verify {
						cstate.EXPECT().AddToCache(gomock.Any()).DoAndReturn(
							func(got *types.Transaction) error {
								assert.Equal(t, tx.ID, got.ID) // causing ID to be calculated
								assert.Equal(t, tx, got)
								return tc.addErr
							}).Times(1)
					}
				}
			}

			require.Equal(t,
				tc.expect,
				th.HandleGossipTransaction(context.TODO(), "peer", tx.Raw),
			)
		})
	}
}
