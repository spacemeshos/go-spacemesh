package weakcoin_test

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	servicemocks "github.com/spacemeshos/go-spacemesh/p2p/service/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/signing/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin/mocks"
	"github.com/stretchr/testify/require"
)

func noopBroadcaster(tb testing.TB, ctrl *gomock.Controller) *mocks.Mockbroadcaster {
	bc := mocks.NewMockbroadcaster(ctrl)
	bc.EXPECT().Broadcast(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	return bc
}

func broadcastedMessage(tb testing.TB, ctrl *gomock.Controller, msg weakcoin.Message) *servicemocks.MockGossipMessage {
	mm := servicemocks.NewMockGossipMessage(ctrl)
	mm.EXPECT().Sender().Return(p2pcrypto.NewRandomPubkey()).AnyTimes()
	buf, err := types.InterfaceToBytes(&msg)
	require.NoError(tb, err)
	mm.EXPECT().Bytes().Return(buf).AnyTimes()
	mm.EXPECT().ReportValidation(gomock.Any(), gomock.Any()).AnyTimes()
	return mm
}

func staticSigner(tb testing.TB, ctrl *gomock.Controller, sig []byte) *smocks.MockSigner {
	signer := smocks.NewMockSigner(ctrl)
	signer.EXPECT().Sign(gomock.Any()).Return(sig).AnyTimes()
	signer.EXPECT().PublicKey().Return(signing.NewPublicKey(sig)).AnyTimes()
	return signer
}

func pubkeyFromSigVerifier(tb testing.TB, ctrl *gomock.Controller) *smocks.MockVerifier {
	verifier := smocks.NewMockVerifier(ctrl)
	verifier.EXPECT().Extract(gomock.Any(), gomock.Any()).DoAndReturn(
		func(msg, sig []byte) (*signing.PublicKey, error) {
			return signing.NewPublicKey(sig), nil
		}).AnyTimes()
	return verifier
}

func TestWeakCoin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		epoch types.EpochID = 10
		round types.RoundID = 4

		oneLSB          = []byte{0b0001}
		zeroLSB         = []byte{0b0110}
		higherThreshold = []byte{0xff}

		verifier = pubkeyFromSigVerifier(t, ctrl)

		config = weakcoin.DefaultConfig()
	)
	config.MaxRound = 10
	config.Threshold = []byte{0xfe}

	tcs := []struct {
		desc         string
		local        []byte
		allowances   weakcoin.UnitAllowances
		startedEpoch types.EpochID
		startedRound types.RoundID
		messages     []weakcoin.Message
		coinflip     bool
	}{
		{
			desc:         "ValidProposalFromNetwork",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(oneLSB): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round,
				Unit:      1,
				Signature: oneLSB,
			}},
			coinflip: true,
		},
		{
			desc:         "LocalProposer",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(oneLSB): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
		},
		{
			desc:         "ProposalFromNetworkNotAllowed",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(oneLSB): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round,
				Unit:      2,
				Signature: oneLSB,
			}},
		},
		{
			desc:         "ProposalFromNetworkHigherThreshold",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(higherThreshold): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round,
				Unit:      1,
				Signature: higherThreshold,
			}},
		},
		{
			desc:         "LocalProposalNotAllowed",
			local:        oneLSB,
			allowances:   weakcoin.UnitAllowances{string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round,
				Unit:      1,
				Signature: zeroLSB,
			}},
		},
		{
			desc:         "PreviousEpoch",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(zeroLSB): 1, string(oneLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch - 1,
				Round:     round,
				Unit:      1,
				Signature: oneLSB,
			}},
		},
		{
			desc:         "PreviousRound",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(zeroLSB): 1, string(oneLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round - 1,
				Unit:      1,
				Signature: oneLSB,
			}},
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local), verifier,
				weakcoin.WithConfig(config))

			wc.StartEpoch(tc.startedEpoch, tc.allowances)
			require.NoError(t, wc.StartRound(context.TODO(), tc.startedRound))

			for _, msg := range tc.messages {
				wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, msg), nil)
			}
			wc.CompleteRound()

			require.Equal(t, tc.coinflip, wc.Get(epoch, round))
		})
	}

	for _, tc := range tcs {
		tc := tc
		t.Run("BufferingStartEpoch/"+tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local), verifier,
				weakcoin.WithConfig(config))

			wc.StartEpoch(tc.startedEpoch, tc.allowances)

			for _, msg := range tc.messages {
				wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, msg), nil)
			}
			require.NoError(t, wc.StartRound(context.TODO(), tc.startedRound))
			wc.CompleteRound()

			require.Equal(t, tc.coinflip, wc.Get(epoch, round))
		})
	}

	for _, tc := range tcs {
		tc := tc
		t.Run("BufferingNextRound/"+tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local), verifier,
				weakcoin.WithConfig(config))

			wc.StartEpoch(tc.startedEpoch, tc.allowances)
			require.NoError(t, wc.StartRound(context.TODO(), tc.startedRound))

			for _, msg := range tc.messages {
				msg.Round++
				wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, msg), nil)
			}

			require.NoError(t, wc.StartRound(context.TODO(), tc.startedRound+1))
			wc.CompleteRound()

			require.Equal(t, tc.coinflip, wc.Get(tc.startedEpoch, tc.startedRound+1))
			wc.CompleteEpoch()
		})
	}
	for _, tc := range tcs {
		tc := tc
		t.Run("BufferingNextEpochAfterCompletion/"+tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local), verifier,
				weakcoin.WithConfig(config))

			wc.StartEpoch(tc.startedEpoch, tc.allowances)
			wc.CompleteEpoch()

			wc.StartEpoch(tc.startedEpoch+1, tc.allowances)
			for _, msg := range tc.messages {
				msg.Epoch++
				wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, msg), nil)
			}

			require.NoError(t, wc.StartRound(context.TODO(), tc.startedRound))
			wc.CompleteRound()

			require.Equal(t, tc.coinflip, wc.Get(tc.startedEpoch+1, tc.startedRound))
			wc.CompleteEpoch()
		})
	}
}

func TestWeakCoinGetPanic(t *testing.T) {
	var (
		ctrl = gomock.NewController(t)
		wc   = weakcoin.New(
			noopBroadcaster(t, ctrl),
			staticSigner(t, ctrl, []byte{1}), pubkeyFromSigVerifier(t, ctrl))
		epoch types.EpochID = 10
		round types.RoundID = 2
	)
	defer ctrl.Finish()

	require.Panics(t, func() {
		wc.Get(epoch, round)
	})

	wc.StartEpoch(epoch, nil)
	require.False(t, wc.Get(epoch, round))
}
