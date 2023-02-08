package weakcoin_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/beacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func noopBroadcaster(tb testing.TB, ctrl *gomock.Controller) *mocks.MockPublisher {
	tb.Helper()
	bc := mocks.NewMockPublisher(ctrl)
	bc.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	return bc
}

func broadcastedMessage(tb testing.TB, msg weakcoin.Message) []byte {
	tb.Helper()
	buf, err := codec.Encode(&msg)
	require.NoError(tb, err)
	return buf
}

func staticSigner(tb testing.TB, ctrl *gomock.Controller, sig []byte) *weakcoin.MockvrfSigner {
	tb.Helper()
	signer := weakcoin.NewMockvrfSigner(ctrl)
	signer.EXPECT().Sign(gomock.Any()).Return(sig).AnyTimes()
	signer.EXPECT().PublicKey().Return(signing.NewPublicKey(sig)).AnyTimes()
	signer.EXPECT().LittleEndian().Return(true).AnyTimes()
	return signer
}

func sigVerifier(tb testing.TB, ctrl *gomock.Controller) *weakcoin.MockvrfVerifier {
	tb.Helper()
	verifier := weakcoin.NewMockvrfVerifier(ctrl)
	verifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	return verifier
}

func nonceFetcher(tb testing.TB, ctrl *gomock.Controller) *weakcoin.MocknonceFetcher {
	tb.Helper()
	fetcher := weakcoin.NewMocknonceFetcher(ctrl)
	fetcher.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(1), nil).AnyTimes()
	return fetcher
}

func TestWeakCoin(t *testing.T) {
	ctrl := gomock.NewController(t)

	var (
		epoch types.EpochID = 10
		round types.RoundID = 4

		oneLSB          = []byte{0b0001}
		zeroLSB         = []byte{0b0110}
		higherThreshold = []byte{0xff}
	)

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
				Epoch:        epoch,
				Round:        round,
				Unit:         1,
				MinerPK:      oneLSB,
				VrfSignature: oneLSB,
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
				Epoch:        epoch,
				Round:        round,
				Unit:         2,
				MinerPK:      oneLSB,
				VrfSignature: oneLSB,
			}},
		},
		{
			desc:         "ProposalFromNetworkHigherThreshold",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(higherThreshold): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:        epoch,
				Round:        round,
				Unit:         1,
				MinerPK:      higherThreshold,
				VrfSignature: higherThreshold,
			}},
		},
		{
			desc:         "LocalProposalHigherThreshold",
			local:        higherThreshold,
			allowances:   weakcoin.UnitAllowances{string(higherThreshold): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:        epoch,
				Round:        round,
				Unit:         1,
				MinerPK:      zeroLSB,
				VrfSignature: zeroLSB,
			}},
		},
		{
			desc:         "LocalProposalNotAllowed",
			local:        oneLSB,
			allowances:   weakcoin.UnitAllowances{string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:        epoch,
				Round:        round,
				Unit:         1,
				MinerPK:      zeroLSB,
				VrfSignature: zeroLSB,
			}},
		},
		{
			desc:         "PreviousEpoch",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(zeroLSB): 1, string(oneLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:        epoch - 1,
				Round:        round,
				Unit:         1,
				MinerPK:      zeroLSB,
				VrfSignature: oneLSB,
			}},
		},
		{
			desc:         "PreviousRound",
			local:        zeroLSB,
			allowances:   weakcoin.UnitAllowances{string(zeroLSB): 1, string(oneLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:        epoch,
				Round:        round - 1,
				Unit:         1,
				MinerPK:      zeroLSB,
				VrfSignature: oneLSB,
			}},
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local),
				sigVerifier(t, ctrl),
				nonceFetcher(t, ctrl),
				weakcoin.WithThreshold([]byte{0xfe}),
			)

			wc.StartEpoch(context.Background(), tc.startedEpoch, types.VRFPostIndex(1), tc.allowances)
			require.NoError(t, wc.StartRound(context.Background(), tc.startedRound))

			for _, msg := range tc.messages {
				wc.HandleProposal(context.Background(), "", broadcastedMessage(t, msg))
			}
			wc.FinishRound(context.Background())

			require.Equal(t, tc.coinflip, wc.Get(context.Background(), tc.startedEpoch, tc.startedRound))
		})
	}

	for _, tc := range tcs {
		tc := tc
		t.Run("BufferingStartEpoch/"+tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local),
				sigVerifier(t, ctrl),
				nonceFetcher(t, ctrl),
				weakcoin.WithThreshold([]byte{0xfe}),
			)

			wc.StartEpoch(context.Background(), tc.startedEpoch, types.VRFPostIndex(1), tc.allowances)

			for _, msg := range tc.messages {
				wc.HandleProposal(context.Background(), "", broadcastedMessage(t, msg))
			}
			require.NoError(t, wc.StartRound(context.Background(), tc.startedRound))
			wc.FinishRound(context.Background())

			require.Equal(t, tc.coinflip, wc.Get(context.Background(), tc.startedEpoch, tc.startedRound))
		})
	}

	for _, tc := range tcs {
		tc := tc
		t.Run("BufferingNextRound/"+tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local),
				sigVerifier(t, ctrl),
				nonceFetcher(t, ctrl),
				weakcoin.WithThreshold([]byte{0xfe}),
			)

			wc.StartEpoch(context.Background(), tc.startedEpoch, types.VRFPostIndex(1), tc.allowances)
			require.NoError(t, wc.StartRound(context.Background(), tc.startedRound))

			for _, msg := range tc.messages {
				msg.Round++
				wc.HandleProposal(context.Background(), "", broadcastedMessage(t, msg))
			}

			require.NoError(t, wc.StartRound(context.Background(), tc.startedRound+1))
			wc.FinishRound(context.Background())

			require.Equal(t, tc.coinflip, wc.Get(context.Background(), tc.startedEpoch, tc.startedRound+1))
			wc.FinishEpoch(context.Background(), tc.startedEpoch)
		})
	}
	for _, tc := range tcs {
		tc := tc
		t.Run("BufferingNextEpochAfterCompletion/"+tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, tc.local),
				sigVerifier(t, ctrl),
				nonceFetcher(t, ctrl),
				weakcoin.WithThreshold([]byte{0xfe}),
			)

			wc.StartEpoch(context.Background(), tc.startedEpoch, types.VRFPostIndex(1), tc.allowances)
			wc.FinishEpoch(context.Background(), tc.startedEpoch)

			wc.StartEpoch(context.Background(), tc.startedEpoch+1, types.VRFPostIndex(1), tc.allowances)
			for _, msg := range tc.messages {
				msg.Epoch++
				wc.HandleProposal(context.Background(), "", broadcastedMessage(t, msg))
			}

			require.NoError(t, wc.StartRound(context.Background(), tc.startedRound))
			wc.FinishRound(context.Background())

			require.Equal(t, tc.coinflip, wc.Get(context.Background(), tc.startedEpoch+1, tc.startedRound))
			wc.FinishEpoch(context.Background(), tc.startedEpoch+1)
		})
	}
}

func TestWeakCoinGetPanic(t *testing.T) {
	var (
		ctrl = gomock.NewController(t)
		wc   = weakcoin.New(
			noopBroadcaster(t, ctrl),
			staticSigner(t, ctrl, []byte{1}),
			sigVerifier(t, ctrl),
			nonceFetcher(t, ctrl),
		)
		epoch types.EpochID = 10
		round types.RoundID = 2
	)

	require.Panics(t, func() {
		wc.Get(context.Background(), epoch, round)
	})

	wc.StartEpoch(context.Background(), epoch, types.VRFPostIndex(1), nil)
	require.False(t, wc.Get(context.Background(), epoch, round))
}

func TestWeakCoinNextRoundBufferOverflow(t *testing.T) {
	var (
		ctrl = gomock.NewController(t)

		oneLSB  = []byte{0b0001}
		zeroLSB = []byte{0b0000}

		epoch     types.EpochID = 10
		round     types.RoundID = 2
		nextRound               = round + 1
		bufSize                 = 10
	)

	wc := weakcoin.New(
		noopBroadcaster(t, ctrl),
		staticSigner(t, ctrl, oneLSB),
		sigVerifier(t, ctrl),
		nonceFetcher(t, ctrl),
		weakcoin.WithNextRoundBufferSize(bufSize),
	)

	wc.StartEpoch(context.Background(), epoch, types.VRFPostIndex(1), weakcoin.UnitAllowances{string(oneLSB): 1, string(zeroLSB): 1})
	require.NoError(t, wc.StartRound(context.Background(), round))
	for i := 0; i < bufSize; i++ {
		wc.HandleProposal(context.Background(), "", broadcastedMessage(t, weakcoin.Message{
			Epoch:        epoch,
			Round:        nextRound,
			Unit:         1,
			MinerPK:      oneLSB,
			VrfSignature: oneLSB,
		}))
	}
	wc.HandleProposal(context.Background(), "", broadcastedMessage(t, weakcoin.Message{
		Epoch:        epoch,
		Round:        nextRound,
		Unit:         1,
		VrfSignature: zeroLSB,
	}))
	wc.FinishRound(context.Background())
	require.NoError(t, wc.StartRound(context.Background(), nextRound))
	wc.FinishRound(context.Background())
	require.True(t, wc.Get(context.Background(), epoch, nextRound))
}

func TestWeakCoinEncodingRegression(t *testing.T) {
	ctrl := gomock.NewController(t)

	var (
		sig   []byte
		epoch types.EpochID = 1
		round types.RoundID = 1
	)
	broadcaster := mocks.NewMockPublisher(ctrl)
	broadcaster.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, _ string, data []byte) error {
		var msg weakcoin.Message
		require.NoError(t, codec.Decode(data, &msg))
		sig = msg.VrfSignature
		return nil
	}).AnyTimes()

	rng := rand.New(rand.NewSource(999))
	signer, err := signing.NewEdSigner(
		signing.WithKeyFromRand(rng),
	)
	require.NoError(t, err)
	vrfSig, err := signer.VRFSigner()
	require.NoError(t, err)

	allowances := weakcoin.UnitAllowances{string(signer.PublicKey().Bytes()): 1}
	instance := weakcoin.New(
		broadcaster,
		vrfSig,
		signing.NewVRFVerifier(),
		nonceFetcher(t, ctrl),
		weakcoin.WithThreshold([]byte{0xff}),
	)
	instance.StartEpoch(context.Background(), epoch, types.VRFPostIndex(1), allowances)
	require.NoError(t, instance.StartRound(context.Background(), round))

	require.Equal(t,
		"95838858f8b318d070117421eda3f0d1db5ab97bc366082c17e771873be5ee963122773526fe0c71ba2188cae33cd3ef2212d0188fd07457727c3624b926bf28f2f9aa0ab85a207070688b47f7c6e10f",
		hex.EncodeToString(sig),
	)
}

func TestWeakCoinExchangeProposals(t *testing.T) {
	ctrl := gomock.NewController(t)

	var (
		instances                          = make([]*weakcoin.WeakCoin, 10)
		broadcasters                       = make([]*mocks.MockPublisher, 10)
		vrfSigners                         = make([]*signing.VRFSigner, 10)
		epochStart, epochEnd types.EpochID = 2, 6
		start, end           types.RoundID = 0, 9
		allowances                         = weakcoin.UnitAllowances{}
		rng                                = rand.New(rand.NewSource(999))
	)

	for i := range instances {
		i := i
		broadcaster := mocks.NewMockPublisher(ctrl)
		broadcaster.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().
			DoAndReturn(func(_ context.Context, _ string, data []byte) error {
				for j := range instances {
					if i == j {
						continue
					}
					instances[j].HandleProposal(context.Background(), "", data)
				}
				return nil
			}).AnyTimes()
		broadcasters[i] = broadcaster

		signer, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
		require.NoError(t, err)
		vrfSigner, err := signer.VRFSigner()
		require.NoError(t, err)

		vrfSigners[i] = vrfSigner
		allowances[string(signer.PublicKey().Bytes())] = 1
	}

	for i := range instances {
		instances[i] = weakcoin.New(
			broadcasters[i],
			vrfSigners[i],
			signing.NewVRFVerifier(),
			nonceFetcher(t, ctrl),
			weakcoin.WithLog(logtest.New(t).Named(fmt.Sprintf("coin=%d", i))),
		)
	}

	for epoch := epochStart; epoch <= epochEnd; epoch++ {
		for _, instance := range instances {
			instance.StartEpoch(context.Background(), epoch, types.VRFPostIndex(1), allowances)
		}
		for current := start; current <= end; current++ {
			for _, instance := range instances {
				require.NoError(t, instance.StartRound(context.Background(), current))
			}
			for _, instance := range instances {
				instance.FinishRound(context.Background())
			}
			rst := instances[0].Get(context.Background(), epoch, current)
			for _, instance := range instances[1:] {
				require.Equal(t, rst, instance.Get(context.Background(), epoch, current), "round %d", current)
			}
		}
		for _, instance := range instances {
			instance.FinishEpoch(context.Background(), epoch)
		}
	}
}

func FuzzMessageConsistency(f *testing.F) {
	tester.FuzzConsistency[weakcoin.Message](f)
}

func FuzzMessageStateSafety(f *testing.F) {
	tester.FuzzSafety[weakcoin.Message](f)
}
