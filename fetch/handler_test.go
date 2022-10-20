package fetch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

type testHandler struct {
	*handler
	cdb *datastore.CachedDB
	mm  *mocks.MockmeshProvider
	mb  *smocks.MockBeaconGetter
}

func createTestHandler(t *testing.T) *testHandler {
	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(t)
	mm := mocks.NewMockmeshProvider(ctrl)
	mb := smocks.NewMockBeaconGetter(ctrl)
	return &testHandler{
		handler: newHandler(cdb, datastore.NewBlobStore(cdb.Database), mm, mb, lg),
		cdb:     cdb,
		mm:      mm,
		mb:      mb,
	}
}

func createLayer(t *testing.T, db *datastore.CachedDB, lid types.LayerID) ([]types.BallotID, []types.BlockID) {
	num := 5
	blts := make([]types.BallotID, 0, num)
	blks := make([]types.BlockID, 0, num)
	for i := 0; i < num; i++ {
		b := types.RandomBallot()
		b.LayerIndex = lid
		b.Signature = signing.NewEdSigner().Sign(b.SignedBytes())
		require.NoError(t, b.Initialize())
		require.NoError(t, ballots.Add(db, b))
		blts = append(blts, b.ID())

		bk := types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{LayerIndex: lid})
		require.NoError(t, blocks.Add(db, bk))
		blks = append(blks, bk.ID())
	}
	return blts, blks
}

func createOpinions(t *testing.T, db *datastore.CachedDB, lid types.LayerID, genCert bool) ([]types.BlockID, types.Hash32) {
	_, blks := createLayer(t, db, lid)
	if genCert {
		require.NoError(t, layers.SetHareOutputWithCert(db, lid, &types.Certificate{BlockID: blks[0]}))
	}
	for i, bid := range blks {
		if i == 0 {
			require.NoError(t, blocks.SetValid(db, bid))
		} else {
			require.NoError(t, blocks.SetInvalid(db, bid))
		}
	}
	aggHash := types.RandomHash()
	require.NoError(t, layers.SetHashes(db, lid.Sub(1), types.RandomHash(), aggHash))
	return blks, aggHash
}

func TestHandleLayerDataReq(t *testing.T) {
	tt := []struct {
		name     string
		emptyLyr bool
	}{
		{
			name: "success",
		},
		{
			name:     "empty layer",
			emptyLyr: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lid := types.NewLayerID(111)
			th := createTestHandler(t)
			blts, blks := createLayer(t, th.cdb, lid)

			out, err := th.handleLayerDataReq(context.TODO(), lid.Bytes())
			require.NoError(t, err)
			var got LayerData
			err = codec.Decode(out, &got)
			require.NoError(t, err)
			require.ElementsMatch(t, blts, got.Ballots)
			require.ElementsMatch(t, blks, got.Blocks)
		})
	}
}

func TestHandleLayerOpinionsReq(t *testing.T) {
	tt := []struct {
		name                string
		verified, requested types.LayerID
		missingCert         bool
	}{
		{
			name:      "all good",
			verified:  types.NewLayerID(111),
			requested: types.NewLayerID(111),
		},
		{
			name:      "not verified",
			verified:  types.NewLayerID(100),
			requested: types.NewLayerID(111),
		},
		{
			name:        "cert missing",
			verified:    types.NewLayerID(111),
			requested:   types.NewLayerID(111),
			missingCert: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			th := createTestHandler(t)
			blks, aggHash := createOpinions(t, th.cdb, tc.requested, !tc.missingCert)

			th.mm.EXPECT().LastVerified().Return(tc.verified)
			out, err := th.handleLayerOpinionsReq(context.TODO(), tc.requested.Bytes())
			require.NoError(t, err)

			var got LayerOpinion
			err = codec.Decode(out, &got)
			require.NoError(t, err)
			require.Equal(t, uint64(0), got.EpochWeight)
			require.Equal(t, aggHash, got.PrevAggHash)
			require.Equal(t, tc.verified, got.Verified)
			if tc.missingCert {
				require.Nil(t, got.Cert)
			} else {
				require.NotNil(t, got.Cert)
				require.Equal(t, blks[0], got.Cert.BlockID)
			}
			if !tc.verified.Before(tc.requested) {
				require.ElementsMatch(t, blks[0:1], got.Valid)
				require.ElementsMatch(t, blks[1:], got.Invalid)
			} else {
				require.Empty(t, got.Valid)
				require.Empty(t, got.Invalid)
			}
		})
	}
}

func TestHandleMeshHashReq(t *testing.T) {
	tt := []struct {
		name        string
		params      [4]uint32 // from, to, delta, steps
		hashMissing bool
		err         error
	}{
		{
			name:   "success",
			params: [4]uint32{7, 23, 5, 4},
		},
		{
			name:        "hash missing",
			params:      [4]uint32{7, 23, 5, 4},
			hashMissing: true,
			err:         sql.ErrNotFound,
		},
		{
			name:   "bad boundary",
			params: [4]uint32{23, 23, 5, 4},
			err:    errBadRequest,
		},
		{
			name:   "not enough steps",
			params: [4]uint32{7, 23, 5, 3},
			err:    errBadRequest,
		},
		{
			name:   "too many steps",
			params: [4]uint32{7, 23, 5, 5},
			err:    errBadRequest,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			th := createTestHandler(t)
			req := &MeshHashRequest{
				From:  types.NewLayerID(tc.params[0]),
				To:    types.NewLayerID(tc.params[1]),
				Delta: tc.params[2],
				Steps: tc.params[3],
			}
			if !tc.hashMissing {
				for lid := req.From; !lid.After(req.To); lid = lid.Add(1) {
					require.NoError(t, layers.SetHashes(th.cdb, lid, types.RandomHash(), types.RandomHash()))
				}
			}
			reqData, err := codec.Encode(req)
			require.NoError(t, err)

			resp, err := th.handleMeshHashReq(context.TODO(), reqData)
			if tc.err == nil {
				require.NoError(t, err)
				got, err := codec.DecodeSlice[types.Hash32](resp)
				require.NoError(t, err)
				require.EqualValues(t, len(got), req.Steps+1)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}

func newAtx(t *testing.T, target types.EpochID) *types.VerifiedActivationTx {
	t.Helper()
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: (target - 1).FirstLayer(),
				PrevATXID:  types.RandomATXID(),
			},
			NumUnits: 2,
		},
	}

	bts, err := atx.InnerBytes()
	require.NoError(t, err)
	atx.Sig = signing.NewEdSigner().Sign(bts)
	vatx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	return vatx
}

func TestHandleEpochInfoReq(t *testing.T) {
	beaconErr := errors.New("beacon unavailable")
	tt := []struct {
		name           string
		missingData    bool
		beaconErr, err error
	}{
		{
			name: "all good",
		},
		{
			name:        "no epoch data",
			missingData: true,
		},
		{
			name:      "beacon missing",
			beaconErr: beaconErr,
			err:       beaconErr,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			th := createTestHandler(t)
			epoch := types.EpochID(11)
			var expected EpochData
			if !tc.missingData {
				for i := 0; i < 10; i++ {
					vatx := newAtx(t, epoch)
					require.NoError(t, atxs.Add(th.cdb, vatx, time.Now()))
					expected.AtxIDs = append(expected.AtxIDs, vatx.ID())
					expected.Weight += vatx.GetWeight()
				}
			}
			if tc.beaconErr == nil {
				beacon := types.RandomBeacon()
				expected.Beacon = beacon
				th.mb.EXPECT().GetBeacon(epoch).Return(beacon, nil)
			} else {
				th.mb.EXPECT().GetBeacon(epoch).Return(types.EmptyBeacon, tc.beaconErr)
			}

			out, err := th.handleEpochInfoReq(context.TODO(), epoch.ToBytes())
			require.ErrorIs(t, err, tc.err)
			if tc.err == nil {
				var got EpochData
				require.NoError(t, codec.Decode(out, &got))
				require.Equal(t, expected.Beacon, got.Beacon)
				require.ElementsMatch(t, expected.AtxIDs, got.AtxIDs)
				require.Equal(t, expected.Weight, got.Weight)
			}
		})
	}
}
