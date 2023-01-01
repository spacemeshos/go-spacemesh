package fetch

import (
	"context"
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
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
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

func createLayer(tb testing.TB, db *datastore.CachedDB, lid types.LayerID) ([]types.BallotID, []types.BlockID) {
	num := 5
	blts := make([]types.BallotID, 0, num)
	blks := make([]types.BlockID, 0, num)
	for i := 0; i < num; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(tb, err)

		b := types.RandomBallot()
		b.LayerIndex = lid
		b.Signature = signer.Sign(b.SignedBytes())
		require.NoError(tb, b.Initialize())
		require.NoError(tb, ballots.Add(db, b))
		blts = append(blts, b.ID())

		bk := types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{LayerIndex: lid})
		require.NoError(tb, blocks.Add(db, bk))
		blks = append(blks, bk.ID())
	}
	return blts, blks
}

func createOpinions(t *testing.T, db *datastore.CachedDB, lid types.LayerID, genCert bool) (types.BlockID, types.Hash32) {
	_, blks := createLayer(t, db, lid)
	certified := types.EmptyBlockID
	if genCert {
		certified = blks[0]
		require.NoError(t, certificates.Add(db, lid, &types.Certificate{BlockID: certified}))
	}
	aggHash := types.RandomHash()
	require.NoError(t, layers.SetHashes(db, lid.Sub(1), types.RandomHash(), aggHash))
	return certified, aggHash
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
		name                       string
		missingCert, multipleCerts bool
	}{
		{
			name: "all good",
		},
		{
			name:        "cert missing",
			missingCert: true,
		},
		{
			name:          "multiple certs",
			multipleCerts: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			th := createTestHandler(t)
			lid := types.NewLayerID(111)
			certified, aggHash := createOpinions(t, th.cdb, lid, !tc.missingCert)
			if tc.multipleCerts {
				require.NoError(t, certificates.Add(th.cdb, lid, &types.Certificate{
					BlockID: types.RandomBlockID(),
				}))
			}

			out, err := th.handleLayerOpinionsReq(context.TODO(), lid.Bytes())
			require.NoError(t, err)

			var got LayerOpinion
			err = codec.Decode(out, &got)
			require.NoError(t, err)
			require.Equal(t, aggHash, got.PrevAggHash)
			if tc.missingCert || tc.multipleCerts {
				require.Nil(t, got.Cert)
			} else {
				require.NotNil(t, got.Cert)
				require.Equal(t, certified, got.Cert.BlockID)
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

func newAtx(t *testing.T, published types.EpochID) *types.VerifiedActivationTx {
	t.Helper()
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: published.FirstLayer(),
				PrevATXID:  types.RandomATXID(),
			},
			NumUnits: 2,
		},
	}

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx.Sig = signer.Sign(atx.SignedBytes())
	vatx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	return vatx
}

func TestHandleEpochInfoReq(t *testing.T) {
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
				}
			}

			out, err := th.handleEpochInfoReq(context.TODO(), epoch.ToBytes())
			require.ErrorIs(t, err, tc.err)
			if tc.err == nil {
				var got EpochData
				require.NoError(t, codec.Decode(out, &got))
				require.ElementsMatch(t, expected.AtxIDs, got.AtxIDs)
			}
		})
	}
}
