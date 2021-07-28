package tortoisebeacon

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
)

type validatorMock struct{}

func (*validatorMock) Validate(signing.PublicKey, *types.NIPost, types.Hash32, uint) error {
	return nil
}

func (*validatorMock) ValidatePost([]byte, *types.Post, *types.PostMetadata, uint) error {
	return nil
}

func TestTortoiseBeacon(t *testing.T) {
	t.Parallel()

	requirer := require.New(t)
	conf := UnitTestConfig()

	mockDB := &mockActivationDB{}
	mockDB.On("GetEpochWeight", mock.AnythingOfType("types.EpochID")).Return(uint64(10), nil, nil)

	mwc := &weakcoin.MockWeakCoin{}
	mwc.On("OnRoundStarted",
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID"))
	mwc.On("OnRoundFinished",
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID"))
	mwc.On("PublishProposal",
		mock.Anything,
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID")).
		Return(nil)
	mwc.On("Get",
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID")).
		Return(true)

	logger := log.NewDefault("TortoiseBeacon")

	genesisTime := time.Now().Add(100 * time.Millisecond)
	ld := 100 * time.Millisecond

	types.SetLayersPerEpoch(1)

	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, log.NewDefault("clock"))
	clock.StartNotifying()

	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layer := types.NewLayerID(3)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, vrfPub, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	requirer.NoError(err)

	minerID := types.NodeID{Key: edPubkey.String(), VRFPublicKey: vrfPub}
	lg := log.NewDefault(minerID.Key[:5])
	idStore := activation.NewIdentityStore(database.NewMemDatabase())
	memesh := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	goldenATXID := types.ATXID(types.HexToHash32("11111"))

	atxdb := activation.NewDB(database.NewMemDatabase(), idStore, memesh, 3, goldenATXID, &validatorMock{}, lg.WithName("atxDB"))
	_ = atxdb

	tb := New(conf, minerID, ld, n1, mockDB, nil, edSgn, signing.VRFVerify, vrfSigner, mwc, clock, logger)
	requirer.NotNil(tb)

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
		theta     float64
		weight    uint64
		threshold int
	}{
		{
			name:      "Case 1",
			theta:     0.5,
			weight:    10,
			threshold: 5,
		},
		{
			name:      "Case 2",
			theta:     0.3,
			weight:    10,
			threshold: 3,
		},
		{
			name:      "Case 3",
			theta:     0.00004,
			weight:    31744,
			threshold: 1,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log: log.NewDefault("TortoiseBeacon"),
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

	tt := []struct {
		name      string
		kappa     uint64
		q         string
		w         uint64
		threshold *big.Float
		err       error
	}{
		{
			name:      "Case 1",
			kappa:     40,
			q:         "1/3",
			w:         60,
			threshold: big.NewFloat(0.5),
			err:       nil,
		},
		{
			name:      "Case 2",
			kappa:     40,
			q:         "1/3",
			w:         0,
			threshold: big.NewFloat(0),
			err:       ErrZeroEpochWeight,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			q, ok := new(big.Rat).SetString(tc.q)
			if !ok {
				panic("bad q parameter")
			}

			tb := TortoiseBeacon{
				Log: log.NewDefault("TortoiseBeacon"),
				config: Config{
					Kappa: tc.kappa,
					Q:     tc.q,
				},
				q: q,
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
		q         string
		w         uint64
		threshold string
	}{
		{
			name:      "Case 1",
			kappa:     40,
			q:         "1/3",
			w:         60,
			threshold: "0x80000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name:      "Case 2",
			kappa:     400000,
			q:         "1/3",
			w:         31744,
			threshold: "0xffffddbb63fcd30f0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name:      "Case 3",
			kappa:     40,
			q:         "0.33",
			w:         60,
			threshold: "0x7f8ece00fe541f0b0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			q, ok := new(big.Rat).SetString(tc.q)
			if !ok {
				panic("bad q parameter")
			}

			tb := TortoiseBeacon{
				Log: log.NewDefault("TortoiseBeacon"),
				config: Config{
					Kappa: tc.kappa,
				},
				vrfSigner: vrfSigner,
				q:         q,
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
		q        string
		w        uint64
		proposal []byte
		passes   bool
	}{
		{
			name:     "Case 1",
			kappa:    400000,
			q:        "1/3",
			w:        31744,
			proposal: util.Hex2Bytes("cee191e87d83dc4fbd5e2d40679accf68237b1f09f73f16db5b05ae74b522def9d2ffee56eeb02070277be99a80666ffef9fd4514a51dc37419dd30a791e0f05"),
			passes:   true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			q, ok := new(big.Rat).SetString(tc.q)
			if !ok {
				panic("bad q parameter")
			}

			tb := TortoiseBeacon{
				Log: log.NewDefault("TortoiseBeacon"),
				config: Config{
					Kappa: tc.kappa,
				},
				vrfSigner: vrfSigner,
				q:         q,
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
			result: "00000003544250000000000012345678",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log: log.NewDefault("TortoiseBeacon"),
			}

			result, err := tb.buildProposal(tc.epoch)
			r.NoError(err)
			r.Equal(tc.result, util.Bytes2Hex(result))
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
				Log:      log.NewDefault("TortoiseBeacon"),
				edSigner: edSgn,
			}

			result, err := tb.signMessage(tc.message)
			r.NoError(err)
			r.Equal(util.Bytes2Hex(tc.result), util.Bytes2Hex(result))
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
			result: vrfSigner.Sign([]byte{0, 0, 0, 3, 84, 66, 80, 0, 0, 0, 0, 0, 0, 0, 0, 1}),
		},
		{
			name:   "Case 2",
			epoch:  2,
			result: vrfSigner.Sign([]byte{0, 0, 0, 3, 84, 66, 80, 0, 0, 0, 0, 0, 0, 0, 0, 2}),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log:       log.NewDefault("TortoiseBeacon"),
				vrfSigner: vrfSigner,
			}

			result, err := tb.getSignedProposal(tc.epoch)
			r.NoError(err)
			r.Equal(util.Bytes2Hex(tc.result), util.Bytes2Hex(result))
		})
	}
}
