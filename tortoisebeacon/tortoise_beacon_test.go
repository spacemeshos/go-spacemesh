package tortoisebeacon

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/mocks"
)

type validatorMock struct{}

func (*validatorMock) Validate(signing.PublicKey, *types.NIPost, types.Hash32, uint) error {
	return nil
}

func (*validatorMock) ValidatePost([]byte, *types.Post, *types.PostMetadata, uint) error {
	return nil
}

type testSyncState bool

func (ss testSyncState) IsSynced(context.Context) bool {
	return bool(ss)
}

func TestTortoiseBeacon(t *testing.T) {
	t.Parallel()

	requirer := require.New(t)
	conf := UnitTestConfig()

	ctrl := gomock.NewController(t)
	mockDB := mocks.NewMockactivationDB(ctrl)
	mockDB.EXPECT().GetEpochWeight(gomock.Any()).Return(uint64(10), nil, nil).AnyTimes()

	mwc := coinValueMock(t, true)

	logger := logtest.New(t).WithName("TortoiseBeacon")

	genesisTime := time.Now().Add(100 * time.Millisecond)
	ld := 100 * time.Millisecond

	types.SetLayersPerEpoch(1)

	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, logtest.New(t).WithName("clock"))
	clock.StartNotifying()

	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layer := types.NewLayerID(3)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, vrfPub, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	requirer.NoError(err)

	minerID := types.NodeID{Key: edPubkey.String(), VRFPublicKey: vrfPub}
	lg := logtest.New(t).WithName(minerID.Key[:5])
	idStore := activation.NewIdentityStore(database.NewMemDatabase())
	memesh := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	goldenATXID := types.ATXID(types.HexToHash32("11111"))

	atxdb := activation.NewDB(database.NewMemDatabase(), idStore, memesh, 3, goldenATXID, &validatorMock{}, lg.WithName("atxDB"))
	_ = atxdb

	tb := New(conf, minerID, n1, mockDB, nil, edSgn, signing.NewEDVerifier(), vrfSigner, signing.VRFVerifier{}, mwc, clock, logger)
	requirer.NotNil(tb)
	tb.SetSyncState(testSyncState(true))

	err = tb.Start(context.TODO())
	requirer.NoError(err)

	t.Logf("Awaiting epoch %v", layer)
	awaitLayer(clock, layer)

	v, err := tb.GetBeacon(layer.GetEpoch())
	requirer.NoError(err)

	expected := "0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	requirer.Equal(expected, types.BytesToHash(v).String())

	tb.Close()
}

func awaitLayer(clock *timesync.TimeClock, epoch types.LayerID) {
	layerTicker := clock.Subscribe()

	for layer := range layerTicker {
		// Wait until required epoch passes.
		if layer.After(epoch) {
			return
		}
	}
}

func TestTortoiseBeacon_votingThreshold(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name      string
		theta     *big.Rat
		weight    uint64
		threshold *big.Int
	}{
		{
			name:      "Case 1",
			theta:     big.NewRat(1, 2),
			weight:    10,
			threshold: big.NewInt(5),
		},
		{
			name:      "Case 2",
			theta:     big.NewRat(3, 10),
			weight:    10,
			threshold: big.NewInt(3),
		},
		{
			name:      "Case 3",
			theta:     big.NewRat(1, 25000),
			weight:    31744,
			threshold: big.NewInt(1),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log: logtest.New(t).WithName("TortoiseBeacon"),
				config: Config{
					Theta: tc.theta,
				},
			}

			threshold := tb.votingThreshold(tc.weight)
			r.EqualValues(tc.threshold, threshold)
		})
	}
}

func TestTortoiseBeacon_atxThresholdFraction(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	theta1, ok := new(big.Float).SetString("0.5")
	r.True(ok)

	theta2, ok := new(big.Float).SetString("0.0")
	r.True(ok)

	tt := []struct {
		name      string
		kappa     uint64
		q         *big.Rat
		w         uint64
		threshold *big.Float
		err       error
	}{
		{
			name:      "Case 1",
			kappa:     40,
			q:         big.NewRat(1, 3),
			w:         60,
			threshold: theta1,
			err:       nil,
		},
		{
			name:      "Case 2",
			kappa:     40,
			q:         big.NewRat(1, 3),
			w:         0,
			threshold: theta2,
			err:       ErrZeroEpochWeight,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log: logtest.New(t).WithName("TortoiseBeacon"),
				config: Config{
					Kappa: tc.kappa,
					Q:     tc.q,
				},
			}

			threshold, err := tb.atxThresholdFraction(tc.w)
			r.Equal(tc.err, err)
			r.Equal(tc.threshold.String(), threshold.String())
		})
	}
}

func TestTortoiseBeacon_atxThreshold(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, _, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	tt := []struct {
		name      string
		kappa     uint64
		q         *big.Rat
		w         uint64
		threshold string
	}{
		{
			name:      "Case 1",
			kappa:     40,
			q:         big.NewRat(1, 3),
			w:         60,
			threshold: "0x80000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name:      "Case 2",
			kappa:     400000,
			q:         big.NewRat(1, 3),
			w:         31744,
			threshold: "0xffffddbb63fcd30f0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name:      "Case 3",
			kappa:     40,
			q:         new(big.Rat).SetFloat64(0.33),
			w:         60,
			threshold: "0x7f8ece00fe541f9f0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log: logtest.New(t).WithName("TortoiseBeacon"),
				config: Config{
					Kappa: tc.kappa,
					Q:     tc.q,
				},
				vrfSigner: vrfSigner,
			}

			threshold, err := tb.atxThreshold(tc.w)
			r.NoError(err)
			r.EqualValues(tc.threshold, fmt.Sprintf("%#x", threshold))
		})
	}
}

func TestTortoiseBeacon_proposalPassesEligibilityThreshold(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, _, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	tt := []struct {
		name     string
		kappa    uint64
		q        *big.Rat
		w        uint64
		proposal []byte
		passes   bool
	}{
		{
			name:     "Case 1",
			kappa:    400000,
			q:        big.NewRat(1, 3),
			w:        31744,
			proposal: util.Hex2Bytes("cee191e87d83dc4fbd5e2d40679accf68237b1f09f73f16db5b05ae74b522def9d2ffee56eeb02070277be99a80666ffef9fd4514a51dc37419dd30a791e0f05"),
			passes:   true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log: logtest.New(t).WithName("TortoiseBeacon"),
				config: Config{
					Kappa: tc.kappa,
					Q:     tc.q,
				},
				vrfSigner: vrfSigner,
			}

			passes, err := tb.proposalPassesEligibilityThreshold(tc.proposal, tc.w)
			r.NoError(err)
			r.EqualValues(tc.passes, passes)
		})
	}
}

func Test_ceilDuration(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name     string
		duration time.Duration
		multiple time.Duration
		result   time.Duration
	}{
		{
			name:     "Case 1",
			duration: 7 * time.Second,
			multiple: 5 * time.Second,
			result:   10 * time.Second,
		},
		{
			name:     "Case 2",
			duration: 5 * time.Second,
			multiple: 5 * time.Second,
			result:   5 * time.Second,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := ceilDuration(tc.duration, tc.multiple)
			r.Equal(tc.result, result)
		})
	}
}

func TestTortoiseBeacon_buildProposal(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name   string
		epoch  types.EpochID
		result string
	}{
		{
			name:   "Case 1",
			epoch:  0x12345678,
			result: string(util.Hex2Bytes("000000035442500012345678")),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log: logtest.New(t).WithName("TortoiseBeacon"),
			}

			result, err := tb.buildProposal(tc.epoch)
			r.NoError(err)
			r.Equal(tc.result, string(result))
		})
	}
}

func TestTortoiseBeacon_signMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	edSgn := signing.NewEdSigner()

	tt := []struct {
		name    string
		message interface{}
		result  []byte
	}{
		{
			name:    "Case 1",
			message: []byte{},
			result:  edSgn.Sign([]byte{0, 0, 0, 0}),
		},
		{
			name:    "Case 2",
			message: &struct{ Test int }{Test: 0x12345678},
			result:  edSgn.Sign([]byte{0x12, 0x34, 0x56, 0x78}),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log:      logtest.New(t).WithName("TortoiseBeacon"),
				edSigner: edSgn,
			}

			result, err := tb.signMessage(tc.message)
			r.NoError(err)
			r.Equal(string(tc.result), string(result))
		})
	}
}

func TestTortoiseBeacon_getSignedProposal(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, _, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	tt := []struct {
		name   string
		epoch  types.EpochID
		result []byte
	}{
		{
			name:   "Case 1",
			epoch:  1,
			result: vrfSigner.Sign(util.Hex2Bytes("000000035442500000000001")),
		},
		{
			name:   "Case 2",
			epoch:  2,
			result: vrfSigner.Sign(util.Hex2Bytes("000000035442500000000002")),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log:       logtest.New(t).WithName("TortoiseBeacon"),
				vrfSigner: vrfSigner,
			}

			result, err := tb.getSignedProposal(context.TODO(), tc.epoch)
			r.NoError(err)
			r.Equal(string(tc.result), string(result))
		})
	}
}

func TestTortoiseBeacon_signAndExtractED(t *testing.T) {
	r := require.New(t)

	signer := signing.NewEdSigner()
	verifier := signing.NewEDVerifier()

	message := []byte{1, 2, 3, 4}

	signature := signer.Sign(message)
	extractedPK, err := verifier.Extract(message, signature)
	r.NoError(err)

	ok := verifier.Verify(extractedPK, message, signature)

	r.Equal(signer.PublicKey().String(), extractedPK.String())
	r.True(ok)
}

func TestTortoiseBeacon_signAndVerifyVRF(t *testing.T) {
	r := require.New(t)

	signer, _, err := signing.NewVRFSigner(bytes.Repeat([]byte{0x01}, 32))
	r.NoError(err)

	verifier := signing.VRFVerifier{}

	message := []byte{1, 2, 3, 4}

	signature := signer.Sign(message)
	ok := verifier.Verify(signer.PublicKey(), message, signature)
	r.True(ok)
}
