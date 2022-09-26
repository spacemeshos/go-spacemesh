package fetch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

type lyrdata struct {
	hash, aggHash types.Hash32
	blts          []types.BallotID
	blks          []types.BlockID
}

type testHandler struct {
	*handler
}

func createTestHandler(t *testing.T) *testHandler {
	db := sql.InMemory()
	return &testHandler{
		handler: newHandler(db, datastore.NewBlobStore(db), logtest.New(t)),
	}
}

func createLayer(t *testing.T, db *sql.Database, lid types.LayerID) *lyrdata {
	l := &lyrdata{}
	l.hash = types.RandomHash()
	l.aggHash = types.RandomHash()
	require.NoError(t, layers.SetHashes(db, lid, l.hash, l.aggHash))
	for i := 0; i < 5; i++ {
		b := types.RandomBallot()
		b.LayerIndex = lid
		b.Signature = signing.NewEdSigner([20]byte{}).Sign(b.Bytes())
		require.NoError(t, b.Initialize())
		require.NoError(t, ballots.Add(db, b))
		l.blts = append(l.blts, b.ID())

		bk := types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{LayerIndex: lid})
		require.NoError(t, blocks.Add(db, bk))
		l.blks = append(l.blks, bk.ID())
	}
	return l
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
			expected := createLayer(t, th.db, lid)

			out, err := th.handleLayerDataReq(context.TODO(), lid.Bytes())
			require.NoError(t, err)
			var got LayerData
			err = codec.Decode(out, &got)
			require.NoError(t, err)
			assert.ElementsMatch(t, expected.blts, got.Ballots)
			assert.ElementsMatch(t, expected.blks, got.Blocks)
			assert.Equal(t, expected.hash, got.Hash)
			assert.Equal(t, expected.aggHash, got.AggregatedHash)
		})
	}
}

func TestHandleLayerOpinionsReq(t *testing.T) {
	tt := []struct {
		name string
		cert *types.Certificate
	}{
		{
			name: "has cert",
			cert: &types.Certificate{BlockID: types.BlockID{1, 2, 3}},
		},
		{
			name: "no cert",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			th := createTestHandler(t)
			lid := types.NewLayerID(111)
			if tc.cert != nil {
				require.NoError(t, layers.SetHareOutputWithCert(th.db, lid, tc.cert))
			}

			out, err := th.handleLayerOpinionsReq(context.TODO(), lid.Bytes())
			require.NoError(t, err)
			var got LayerOpinions
			err = codec.Decode(out, &got)
			require.NoError(t, err)
			assert.Equal(t, tc.cert, got.Cert)
		})
	}
}
