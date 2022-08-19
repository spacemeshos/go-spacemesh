package fetch

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
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

type lyrdata struct {
	hash, aggHash types.Hash32
	blts          []types.BallotID
	blks          []types.BlockID
}

type testHandler struct {
	*handler
	mmp *mocks.MockmeshProvider
}

func createTestHandler(t *testing.T) *testHandler {
	mmp := mocks.NewMockmeshProvider(gomock.NewController(t))
	db := sql.InMemory()
	return &testHandler{
		handler: newHandler(db, datastore.NewBlobStore(db), mmp, logtest.New(t)),
		mmp:     mmp,
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
		b.Signature = signing.NewEdSigner().Sign(b.Bytes())
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
		name                    string
		requested               types.LayerID
		emptyLyr, hareHasOutput bool
	}{
		{
			name:          "success",
			requested:     types.NewLayerID(100),
			hareHasOutput: true,
		},
		{
			name:          "empty hare output",
			requested:     types.NewLayerID(100),
			hareHasOutput: false,
		},
		{
			name:      "empty layer",
			requested: types.NewLayerID(100),
			emptyLyr:  true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.hareHasOutput {
				require.False(t, tc.emptyLyr)
			}
			th := createTestHandler(t)
			expected := &lyrdata{}
			if !tc.emptyLyr {
				expected = createLayer(t, th.db, tc.requested)
			}
			hareOutput := types.EmptyBlockID
			if tc.hareHasOutput {
				hareOutput = expected.blks[0]
			}
			require.NoError(t, layers.SetHareOutput(th.db, tc.requested, hareOutput))

			out, err := th.handleLayerDataReq(context.TODO(), tc.requested.Bytes())
			require.NoError(t, err)
			var got layerData
			err = codec.Decode(out, &got)
			require.NoError(t, err)
			assert.ElementsMatch(t, expected.blts, got.Ballots)
			assert.ElementsMatch(t, expected.blks, got.Blocks)
			assert.Equal(t, hareOutput, got.HareOutput)
			assert.Equal(t, expected.hash, got.Hash)
			assert.Equal(t, expected.aggHash, got.AggregatedHash)
		})
	}
}
