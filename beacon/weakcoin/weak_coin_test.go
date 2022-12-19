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
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
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

func staticSigner(tb testing.TB, ctrl *gomock.Controller, sig []byte) *signing.MockSigner {
	tb.Helper()
	signer := signing.NewMockSigner(ctrl)
	signer.EXPECT().Sign(gomock.Any()).Return(sig).AnyTimes()
	signer.EXPECT().PublicKey().Return(signing.NewPublicKey(sig)).AnyTimes()
	signer.EXPECT().LittleEndian().Return(true).AnyTimes()
	return signer
}

func sigVerifier(tb testing.TB, ctrl *gomock.Controller) *signing.MockVerifier {
	tb.Helper()
	verifier := signing.NewMockVerifier(ctrl)
	verifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	return verifier
}

func TestWeakCoin(t *testing.T) {
	ctrl := gomock.NewController(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t).WithName("weakcoin"))

	var (
		epoch types.EpochID = 10
		round types.RoundID = 4

		oneLSB          = []byte{0b0001}
		zeroLSB         = []byte{0b0110}
		higherThreshold = []byte{0xff}

		verifier = sigVerifier(t, ctrl)
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
				Epoch:     epoch,
				Round:     round,
				Unit:      1,
				MinerPK:   oneLSB,
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
				MinerPK:   oneLSB,
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
				MinerPK:   higherThreshold,
				Signature: higherThreshold,
			}},
		},
		{
			desc:         "LocalProposalHigherThreshold",
			local:        higherThreshold,
			allowances:   weakcoin.UnitAllowances{string(higherThreshold): 1, string(zeroLSB): 1},
			startedEpoch: epoch,
			startedRound: round,
			messages: []weakcoin.Message{{
				Epoch:     epoch,
				Round:     round,
				Unit:      1,
				MinerPK:   zeroLSB,
				Signature: zeroLSB,
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
				MinerPK:   zeroLSB,
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
				MinerPK:   zeroLSB,
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
				MinerPK:   zeroLSB,
				Signature: oneLSB,
			}},
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				cdb,
				staticSigner(t, ctrl, tc.local),
				weakcoin.WithThreshold([]byte{0xfe}),
				weakcoin.WithVerifier(verifier),
			)

			wc.StartEpoch(context.Background(), tc.startedEpoch, tc.allowances)
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
				cdb,
				staticSigner(t, ctrl, tc.local),
				weakcoin.WithThreshold([]byte{0xfe}),
				weakcoin.WithVerifier(verifier),
			)

			wc.StartEpoch(context.Background(), tc.startedEpoch, tc.allowances)

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
				cdb,
				staticSigner(t, ctrl, tc.local),
				weakcoin.WithThreshold([]byte{0xfe}),
				weakcoin.WithVerifier(verifier),
			)

			wc.StartEpoch(context.Background(), tc.startedEpoch, tc.allowances)
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
				cdb,
				staticSigner(t, ctrl, tc.local),
				weakcoin.WithThreshold([]byte{0xfe}),
				weakcoin.WithVerifier(verifier),
			)

			wc.StartEpoch(context.Background(), tc.startedEpoch, tc.allowances)
			wc.FinishEpoch(context.Background(), tc.startedEpoch)

			wc.StartEpoch(context.Background(), tc.startedEpoch+1, tc.allowances)
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
		cdb  = datastore.NewCachedDB(sql.InMemory(), logtest.New(t).WithName("weakcoin"))
		wc   = weakcoin.New(
			noopBroadcaster(t, ctrl),
			cdb,
			staticSigner(t, ctrl, []byte{1}),
			weakcoin.WithVerifier(sigVerifier(t, ctrl)))
		epoch types.EpochID = 10
		round types.RoundID = 2
	)

	require.Panics(t, func() {
		wc.Get(context.Background(), epoch, round)
	})

	wc.StartEpoch(context.Background(), epoch, nil)
	require.False(t, wc.Get(context.Background(), epoch, round))
}

func TestWeakCoinNextRoundBufferOverflow(t *testing.T) {
	var (
		ctrl = gomock.NewController(t)
		cdb  = datastore.NewCachedDB(sql.InMemory(), logtest.New(t).WithName("weakcoin"))

		oneLSB  = []byte{0b0001}
		zeroLSB = []byte{0b0000}

		epoch     types.EpochID = 10
		round     types.RoundID = 2
		nextRound               = round + 1
		bufSize                 = 10
	)

	wc := weakcoin.New(
		noopBroadcaster(t, ctrl),
		cdb,
		staticSigner(t, ctrl, oneLSB),
		weakcoin.WithNextRoundBufferSize(bufSize),
		weakcoin.WithVerifier(sigVerifier(t, ctrl)),
	)

	wc.StartEpoch(context.Background(), epoch, weakcoin.UnitAllowances{string(oneLSB): 1, string(zeroLSB): 1})
	require.NoError(t, wc.StartRound(context.Background(), round))
	for i := 0; i < bufSize; i++ {
		wc.HandleProposal(context.Background(), "", broadcastedMessage(t, weakcoin.Message{
			Epoch:     epoch,
			Round:     nextRound,
			Unit:      1,
			MinerPK:   oneLSB,
			Signature: oneLSB,
		}))
	}
	wc.HandleProposal(context.Background(), "", broadcastedMessage(t, weakcoin.Message{
		Epoch:     epoch,
		Round:     nextRound,
		Unit:      1,
		Signature: zeroLSB,
	}))
	wc.FinishRound(context.Background())
	require.NoError(t, wc.StartRound(context.Background(), nextRound))
	wc.FinishRound(context.Background())
	require.True(t, wc.Get(context.Background(), epoch, nextRound))
}

func TestWeakCoinEncodingRegression(t *testing.T) {
	ctrl := gomock.NewController(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t).WithName("weakcoin"))

	var (
		sig   []byte
		epoch types.EpochID = 1
		round types.RoundID = 1
	)
	broadcaster := mocks.NewMockPublisher(ctrl)
	broadcaster.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, _ string, data []byte) error {
		msg := weakcoin.Message{}
		require.NoError(t, codec.Decode(data, &msg))
		sig = msg.Signature
		return nil
	}).AnyTimes()
	r := rand.New(rand.NewSource(999))

	signer := signing.NewEdSignerFromRand(r).VRFSigner()

	allowances := weakcoin.UnitAllowances{string(signer.PublicKey().Bytes()): 1}
	instance := weakcoin.New(broadcaster, cdb, signer, weakcoin.WithThreshold([]byte{0xff}))
	instance.StartEpoch(context.Background(), epoch, allowances)
	require.NoError(t, instance.StartRound(context.Background(), round))

	require.Equal(t,
		"110b3a848728d3c83ba99804e825f56763d190a3a8f13382bf4e31eaabedbfe9a6f20e7dcd4ce5dcecd325b3cf29529415c9c0692abeb3c0f3600f852444f723018863c0fc541b5644dcafb0c0b4c10b",
		hex.EncodeToString(sig))
}

func TestWeakCoinExchangeProposals(t *testing.T) {
	ctrl := gomock.NewController(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t).WithName("weakcoin"))

	var (
		instances                          = make([]*weakcoin.WeakCoin, 10)
		verifier                           = signing.VRFVerifier{}
		epochStart, epochEnd types.EpochID = 2, 6
		start, end           types.RoundID = 0, 9
		allowances                         = weakcoin.UnitAllowances{}
		r                                  = rand.New(rand.NewSource(999))
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
		signer := signing.NewEdSignerFromRand(r).VRFSigner()
		allowances[string(signer.PublicKey().Bytes())] = 1
		instances[i] = weakcoin.New(
			broadcaster,
			cdb,
			signer,
			weakcoin.WithLog(logtest.New(t).Named(fmt.Sprintf("coin=%d", i))),
			weakcoin.WithVerifier(verifier),
		)
	}

	for epoch := epochStart; epoch <= epochEnd; epoch++ {
		for _, instance := range instances {
			instance.StartEpoch(context.Background(), epoch, allowances)
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
