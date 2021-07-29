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
	"github.com/stretchr/testify/assert"
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

func TestWeakCoinNextRoundBufferOverflow(t *testing.T) {
	var (
		ctrl = gomock.NewController(t)

		oneLSB  = []byte{0b0001}
		zeroLSB = []byte{0b0000}

		config = weakcoin.DefaultConfig()

		epoch     types.EpochID = 10
		round     types.RoundID = 2
		nextRound               = round + 1
	)
	config.MaxRound = 10
	config.NextRoundBufferSize = 10
	defer ctrl.Finish()

	wc := weakcoin.New(
		noopBroadcaster(t, ctrl),
		staticSigner(t, ctrl, oneLSB), pubkeyFromSigVerifier(t, ctrl),
		weakcoin.WithConfig(config),
	)

	wc.StartEpoch(epoch, weakcoin.UnitAllowances{string(oneLSB): 1, string(zeroLSB): 1})
	require.NoError(t, wc.StartRound(context.TODO(), round))
	for i := 0; i < config.NextRoundBufferSize; i++ {
		wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, weakcoin.Message{
			Epoch:     epoch,
			Round:     nextRound,
			Unit:      1,
			Signature: oneLSB,
		}), nil)
	}
	wc.HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, weakcoin.Message{
		Epoch:     epoch,
		Round:     nextRound,
		Unit:      1,
		Signature: zeroLSB,
	}), nil)
	wc.CompleteRound()
	require.NoError(t, wc.StartRound(context.TODO(), nextRound))
	wc.CompleteRound()
	require.True(t, wc.Get(epoch, nextRound))
}

func TestWeakCoinExchangeProposals(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		instances                = make([]*weakcoin.WeakCoin, 10)
		verifier                 = signing.VRFVerifier{}
		epoch      types.EpochID = 2
		start, end types.RoundID = 2, 9
		allowances               = weakcoin.UnitAllowances{}
	)
	for i := range instances {
		i := i
		broadcaster := mocks.NewMockbroadcaster(ctrl)
		broadcaster.EXPECT().Broadcast(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, _ string, data []byte) {
			msg := weakcoin.Message{}
			require.NoError(t, types.BytesToInterface(data, &msg))
			for j := range instances {
				if i == j {
					continue
				}
				instances[j].HandleSerializedMessage(context.TODO(), broadcastedMessage(t, ctrl, msg), nil)
			}
		}).AnyTimes()
		signer := signing.NewEdSigner()
		allowances[signer.PublicKey().String()] = 1
		instances[i] = weakcoin.New(broadcaster, signer, verifier)
	}

	for _, instance := range instances {
		instance.StartEpoch(epoch, allowances)
	}
	for current := start; current <= end; current++ {
		for _, instance := range instances {
			require.NoError(t, instance.StartRound(context.TODO(), current))
		}
		for _, instance := range instances {
			instance.CompleteRound()
		}
		rst := instances[0].Get(epoch, current)
		for _, instance := range instances[1:] {
			assert.Equal(t, rst, instance.Get(epoch, current))
		}
	}
}
