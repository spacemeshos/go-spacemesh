package fetch

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

type testHandler struct {
	*handler
	cdb *datastore.CachedDB
	mm  *mocks.MockmeshProvider
}

func createTestHandler(t *testing.T) *testHandler {
	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	mm := mocks.NewMockmeshProvider(gomock.NewController(t))
	return &testHandler{
		handler: newHandler(cdb, datastore.NewBlobStore(cdb.Database), mm, lg),
		cdb:     cdb,
		mm:      mm,
	}
}

func createLayer(t *testing.T, db *datastore.CachedDB, lid types.LayerID) ([]types.BallotID, []types.BlockID) {
	num := 5
	blts := make([]types.BallotID, 0, num)
	blks := make([]types.BlockID, 0, num)
	for i := 0; i < num; i++ {
		b := types.RandomBallot()
		b.LayerIndex = lid
		b.Signature = signing.NewEdSigner().Sign(b.Bytes())
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
