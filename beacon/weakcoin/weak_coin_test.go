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
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func noopBroadcaster(tb testing.TB, ctrl *gomock.Controller) *mocks.MockPublisher {
	tb.Helper()
	bc := mocks.NewMockPublisher(ctrl)
	bc.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	return bc
}

func encoded(tb testing.TB, msg weakcoin.Message) []byte {
	tb.Helper()
	buf, err := codec.Encode(&msg)
	require.NoError(tb, err)
	return buf
}

func staticSigner(tb testing.TB, ctrl *gomock.Controller, nodeId types.NodeID, sig types.VrfSignature) *weakcoin.MockvrfSigner {
	tb.Helper()
	signer := weakcoin.NewMockvrfSigner(ctrl)
	signer.EXPECT().Sign(gomock.Any()).Return(sig).AnyTimes()
	signer.EXPECT().NodeID().Return(nodeId).AnyTimes()
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
	var (
		ctrl                       = gomock.NewController(t)
		epoch        types.EpochID = 10
		round        types.RoundID = 4
		oneLSBSig                  = types.VrfSignature{0b0001}
		zeroLSBMiner               = types.NodeID{0b0110}
		zeroLSBSig                 = types.VrfSignature{0b0110}
		highLSBSig                 = types.VrfSignature{0xff}
	)

	for _, tc := range []struct {
		desc             string
		nodeSig          types.VrfSignature
		mining, expected bool
		msg              []byte
		result           pubsub.ValidationResult
	}{
		{
			desc:     "node not mining",
			nodeSig:  oneLSBSig,
			mining:   false,
			expected: false,
			msg: encoded(t, weakcoin.Message{
				Epoch:        epoch,
				Round:        round,
				Unit:         1,
				NodeID:       zeroLSBMiner,
				VrfSignature: zeroLSBSig,
			}),
			result: pubsub.ValidationAccept,
		},
		{
			desc:     "node mining",
			nodeSig:  oneLSBSig,
			mining:   true,
			expected: true,
			msg: encoded(t, weakcoin.Message{
				Epoch:        epoch,
				Round:        round,
				Unit:         1,
				NodeID:       zeroLSBMiner,
				VrfSignature: zeroLSBSig,
			}),
			result: pubsub.ValidationIgnore,
		},
		{
			desc:     "node mining but exceed threshold",
			nodeSig:  highLSBSig,
			mining:   true,
			expected: false,
			msg: encoded(t, weakcoin.Message{
				Epoch:        epoch,
				Round:        round,
				Unit:         1,
				NodeID:       zeroLSBMiner,
				VrfSignature: zeroLSBSig,
			}),
			result: pubsub.ValidationAccept,
		},
		{
			desc:     "node only miner",
			nodeSig:  oneLSBSig,
			mining:   true,
			expected: true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			miner := 0
			if tc.mining {
				// once for generating, once for pubsub validation
				miner += 2
			}
			if len(tc.msg) > 0 {
				miner++
			}
			mockAllowance := weakcoin.NewMockallowance(ctrl)
			mockAllowance.EXPECT().MinerAllowance(epoch, gomock.Any()).Return(uint32(1)).MaxTimes(miner)
			var wc *weakcoin.WeakCoin
			mockPublisher := mocks.NewMockPublisher(ctrl)
			mockPublisher.EXPECT().Publish(gomock.Any(), pubsub.BeaconWeakCoinProtocol, gomock.Any()).DoAndReturn(
				func(ctx context.Context, _ string, msg []byte) pubsub.ValidationResult {
					return wc.HandleProposal(ctx, "", msg)
				},
			).AnyTimes()
			wc = weakcoin.New(
				mockPublisher,
				staticSigner(t, ctrl, types.RandomNodeID(), tc.nodeSig),
				sigVerifier(t, ctrl),
				nonceFetcher(t, ctrl),
				mockAllowance,
				weakcoin.WithThreshold([]byte{0xfe}),
				weakcoin.WithLog(logtest.New(t)),
			)

			wc.StartEpoch(context.Background(), epoch)
			nonce := types.VRFPostIndex(1)
			if tc.mining {
				wc.StartRound(context.Background(), round, &nonce)
			} else {
				wc.StartRound(context.Background(), round, nil)
			}

			if len(tc.msg) > 0 {
				require.Equal(t, tc.result, wc.HandleProposal(context.Background(), "", tc.msg))
			}
			wc.FinishRound(context.Background())

			flip, err := wc.Get(context.Background(), epoch, round)
			require.NoError(t, err)
			require.Equal(t, tc.expected, flip)
		})
	}
}

func TestWeakCoin_HandleProposal(t *testing.T) {
	ctrl := gomock.NewController(t)

	var (
		epoch     types.EpochID = 10
		round     types.RoundID = 4
		allowance uint32        = 1

		oneLSBMiner  = types.NodeID{0b0001}
		oneLSBSig    = types.VrfSignature{0b0001}
		zeroLSBSig   = types.VrfSignature{0b0110}
		highLSBMiner = types.NodeID{0xff}
		highLSBSig   = types.VrfSignature{0xff}
	)

	tcs := []struct {
		desc         string
		startedEpoch types.EpochID
		startedRound types.RoundID
		msg          []byte
		expected     pubsub.ValidationResult
	}{
		{
			desc:         "ValidProposal",
			startedEpoch: epoch,
			startedRound: round,
			msg: encoded(t, weakcoin.Message{
				Epoch:        epoch,
				Round:        round,
				Unit:         allowance,
				NodeID:       oneLSBMiner,
				VrfSignature: oneLSBSig,
			}),
			expected: pubsub.ValidationAccept,
		},
		{
			desc:         "Malformed",
			startedEpoch: epoch,
			startedRound: round,
			msg:          []byte{1, 2, 3},
			expected:     pubsub.ValidationReject,
		},
		{
			desc:         "ExceedAllowance",
			startedEpoch: epoch,
			startedRound: round,
			msg: encoded(t, weakcoin.Message{
				Epoch:        epoch,
				Round:        round,
				Unit:         allowance + 1,
				NodeID:       oneLSBMiner,
				VrfSignature: oneLSBSig,
			}),
			expected: pubsub.ValidationIgnore,
		},
		{
			desc:         "ExceedThreshold",
			startedEpoch: epoch,
			startedRound: round,
			msg: encoded(t, weakcoin.Message{
				Epoch:        epoch,
				Round:        round,
				Unit:         allowance,
				NodeID:       highLSBMiner,
				VrfSignature: highLSBSig,
			}),
			expected: pubsub.ValidationIgnore,
		},
		{
			desc:         "PreviousEpoch",
			startedEpoch: epoch,
			startedRound: round,
			msg: encoded(t, weakcoin.Message{
				Epoch:        epoch - 1,
				Round:        round,
				Unit:         allowance,
				NodeID:       oneLSBMiner,
				VrfSignature: oneLSBSig,
			}),
			expected: pubsub.ValidationIgnore,
		},
		{
			desc:         "NextEpoch",
			startedEpoch: epoch,
			startedRound: round,
			msg: encoded(t, weakcoin.Message{
				Epoch:        epoch + 1,
				Round:        round,
				Unit:         allowance,
				NodeID:       oneLSBMiner,
				VrfSignature: oneLSBSig,
			}),
			expected: pubsub.ValidationIgnore,
		},
		{
			desc:         "PreviousRound",
			startedEpoch: epoch,
			startedRound: round,
			msg: encoded(t, weakcoin.Message{
				Epoch:        epoch,
				Round:        round - 1,
				Unit:         allowance,
				NodeID:       oneLSBMiner,
				VrfSignature: oneLSBSig,
			}),
			expected: pubsub.ValidationIgnore,
		},
		{
			desc:         "NextRound",
			startedEpoch: epoch,
			startedRound: round,
			msg: encoded(t, weakcoin.Message{
				Epoch:        epoch,
				Round:        round + 1,
				Unit:         allowance,
				NodeID:       oneLSBMiner,
				VrfSignature: oneLSBSig,
			}),
			expected: pubsub.ValidationAccept,
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			mockAllowance := weakcoin.NewMockallowance(gomock.NewController(t))
			mockAllowance.EXPECT().MinerAllowance(epoch, gomock.Any()).Return(allowance).AnyTimes()
			wc := weakcoin.New(
				noopBroadcaster(t, ctrl),
				staticSigner(t, ctrl, types.RandomNodeID(), zeroLSBSig),
				sigVerifier(t, ctrl),
				nonceFetcher(t, ctrl),
				mockAllowance,
				weakcoin.WithThreshold([]byte{0xfe}),
				weakcoin.WithLog(logtest.New(t)),
			)

			wc.StartEpoch(context.Background(), tc.startedEpoch)
			wc.StartRound(context.Background(), tc.startedRound, nil)

			require.Equal(t, tc.expected, wc.HandleProposal(context.Background(), "", tc.msg))
			wc.FinishRound(context.Background())
		})
	}
}

func TestWeakCoinNextRoundBufferOverflow(t *testing.T) {
	var (
		ctrl = gomock.NewController(t)

		oneLSBMiner = types.NodeID{0b0001}
		oneLSBSig   = types.VrfSignature{0b0001}
		zeroLSBSig  = types.VrfSignature{0b0000}

		epoch     types.EpochID = 10
		round     types.RoundID = 2
		nextRound               = round + 1
		bufSize                 = 10
	)

	mockAllowance := weakcoin.NewMockallowance(gomock.NewController(t))
	mockAllowance.EXPECT().MinerAllowance(epoch, gomock.Any()).Return(uint32(1)).AnyTimes()
	wc := weakcoin.New(
		noopBroadcaster(t, ctrl),
		staticSigner(t, ctrl, types.RandomNodeID(), oneLSBSig),
		sigVerifier(t, ctrl),
		nonceFetcher(t, ctrl),
		mockAllowance,
		weakcoin.WithNextRoundBufferSize(bufSize),
	)

	wc.StartEpoch(context.Background(), epoch)
	wc.StartRound(context.Background(), round, nil)
	for i := 0; i < bufSize; i++ {
		wc.HandleProposal(context.Background(), "", encoded(t, weakcoin.Message{
			Epoch:        epoch,
			Round:        nextRound,
			Unit:         1,
			NodeID:       oneLSBMiner,
			VrfSignature: oneLSBSig,
		}))
	}
	wc.HandleProposal(context.Background(), "", encoded(t, weakcoin.Message{
		Epoch:        epoch,
		Round:        nextRound,
		Unit:         1,
		VrfSignature: zeroLSBSig,
	}))
	wc.FinishRound(context.Background())
	wc.StartRound(context.Background(), nextRound, nil)
	wc.FinishRound(context.Background())
	flip, err := wc.Get(context.Background(), epoch, nextRound)
	require.NoError(t, err)
	require.True(t, flip)
}

func TestWeakCoinEncodingRegression(t *testing.T) {
	ctrl := gomock.NewController(t)

	var (
		sig   types.VrfSignature
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

	mockAllowance := weakcoin.NewMockallowance(gomock.NewController(t))
	mockAllowance.EXPECT().MinerAllowance(epoch, gomock.Any()).DoAndReturn(
		func(_ types.EpochID, miner types.NodeID) uint32 {
			if miner == signer.NodeID() {
				return 1
			}
			return 0
		})
	instance := weakcoin.New(
		broadcaster,
		vrfSig,
		signing.NewVRFVerifier(),
		nonceFetcher(t, ctrl),
		mockAllowance,
		weakcoin.WithThreshold([]byte{0xff}),
		weakcoin.WithLog(logtest.New(t)),
	)
	instance.StartEpoch(context.Background(), epoch)
	nonce := types.VRFPostIndex(1)
	instance.StartRound(context.Background(), round, &nonce)

	require.Equal(t,
		"78f523319fd2cdf3812a3bc3905561acb2f7f1b7e47de71f92811d7bb82460e5999a048051cefa2d1b6f3f16656de83c2756b7539b33fa563a3e8fea5130235e66e8dce914d69bd40f13174f3914ad07",
		hex.EncodeToString(sig[:]),
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
		rng                                = rand.New(rand.NewSource(999))
	)

	for i := range instances {
		i := i
		broadcaster := mocks.NewMockPublisher(ctrl)
		broadcaster.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().
			DoAndReturn(func(_ context.Context, _ string, data []byte) error {
				for j := range instances {
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
	}

	mockAllowance := weakcoin.NewMockallowance(gomock.NewController(t))
	mockAllowance.EXPECT().MinerAllowance(gomock.Any(), gomock.Any()).Return(uint32(1)).AnyTimes()

	for i := range instances {
		instances[i] = weakcoin.New(
			broadcasters[i],
			vrfSigners[i],
			signing.NewVRFVerifier(),
			nonceFetcher(t, ctrl),
			mockAllowance,
			weakcoin.WithLog(logtest.New(t).Named(fmt.Sprintf("coin=%d", i))),
		)
	}

	nonce := types.VRFPostIndex(1)
	for epoch := epochStart; epoch <= epochEnd; epoch++ {
		for _, instance := range instances {
			instance.StartEpoch(context.Background(), epoch)
		}
		for current := start; current <= end; current++ {
			for i, instance := range instances {
				if i == 0 {
					instance.StartRound(context.Background(), current, nil)
				} else {
					instance.StartRound(context.Background(), current, &nonce)
				}
			}
			for _, instance := range instances {
				instance.FinishRound(context.Background())
			}
			rst, err := instances[0].Get(context.Background(), epoch, current)
			require.NoError(t, err)
			for _, instance := range instances[1:] {
				got, err := instance.Get(context.Background(), epoch, current)
				require.NoError(t, err)
				require.Equal(t, rst, got, "round %d", current)
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
