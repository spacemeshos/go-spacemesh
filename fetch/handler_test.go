package fetch

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type testHandler struct {
	*handler
	db  sql.StateDatabase
	cdb *datastore.CachedDB
}

func createTestHandler(t testing.TB, opts ...sql.Opt) *testHandler {
	lg := logtest.New(t)
	db := statesql.InMemory(opts...)
	cdb := datastore.NewCachedDB(db, lg.Zap())
	return &testHandler{
		handler: newHandler(cdb, datastore.NewBlobStore(cdb, store.New()), lg),
		db:      db,
		cdb:     cdb,
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
		b.Layer = lid
		b.Signature = signer.Sign(signing.BALLOT, b.SignedBytes())
		b.SmesherID = signer.NodeID()
		require.NoError(tb, b.Initialize())
		require.NoError(tb, ballots.Add(db, b))
		blts = append(blts, b.ID())

		bk := types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{LayerIndex: lid})
		require.NoError(tb, blocks.Add(db, bk))
		blks = append(blks, bk.ID())
	}
	return blts, blks
}

func createOpinions(
	t *testing.T,
	db *datastore.CachedDB,
	lid types.LayerID,
	genCert bool,
) (types.BlockID, types.Hash32) {
	_, blks := createLayer(t, db, lid)
	certified := types.EmptyBlockID
	if genCert {
		certified = blks[0]
		require.NoError(t, certificates.Add(db, lid, &types.Certificate{BlockID: certified}))
	}
	aggHash := types.RandomHash()
	require.NoError(t, layers.SetMeshHash(db, lid.Sub(1), aggHash))
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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lid := types.LayerID(111)
			th := createTestHandler(t)
			blts, _ := createLayer(t, th.cdb, lid)

			lidBytes, err := codec.Encode(&lid)
			require.NoError(t, err)

			out, err := th.handleLayerDataReq(context.Background(), lidBytes)
			require.NoError(t, err)
			var got LayerData
			err = codec.Decode(out, &got)
			require.NoError(t, err)
			require.ElementsMatch(t, blts, got.Ballots)
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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			th := createTestHandler(t)
			lid := types.LayerID(111)
			_, aggHash := createOpinions(t, th.cdb, lid, !tc.missingCert)
			if tc.multipleCerts {
				bid := types.RandomBlockID()
				require.NoError(t, certificates.Add(th.cdb, lid, &types.Certificate{
					BlockID: bid,
				}))
				require.NoError(t, certificates.SetInvalid(th.cdb, lid, bid))
			}

			req := OpinionRequest{Layer: lid}
			reqBytes, err := codec.Encode(&req)
			require.NoError(t, err)

			out, err := th.handleLayerOpinionsReq2(context.Background(), reqBytes)
			require.NoError(t, err)

			var got LayerOpinion
			err = codec.Decode(out, &got)
			require.NoError(t, err)
			require.Equal(t, aggHash, got.PrevAggHash)
			if tc.missingCert {
				require.Nil(t, got.Certified)
			} else {
				require.NotNil(t, got.Certified)
			}
		})
	}
}

func TestHandleCertReq(t *testing.T) {
	th := createTestHandler(t)
	lid := types.LayerID(111)
	bid := types.RandomBlockID()
	req := &OpinionRequest{
		Layer: lid,
		Block: &bid,
	}
	reqData, err := codec.Encode(req)
	require.NoError(t, err)

	resp, err := th.handleLayerOpinionsReq2(context.Background(), reqData)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, resp)

	cert := &types.Certificate{BlockID: bid}
	require.NoError(t, certificates.Add(th.cdb, lid, cert))

	resp, err = th.handleLayerOpinionsReq2(context.Background(), reqData)
	require.NoError(t, err)
	require.NotNil(t, resp)
	var got types.Certificate
	require.NoError(t, codec.Decode(resp, &got))
	require.Equal(t, *cert, got)
}

func TestHandleMeshHashReq(t *testing.T) {
	tt := []struct {
		name        string
		params      [3]uint32 // from, to, delta, steps
		hashMissing bool
		err         error
	}{
		{
			name:   "success",
			params: [3]uint32{7, 23, 5},
		},
		{
			name:        "hash missing",
			params:      [3]uint32{7, 23, 5},
			hashMissing: true,
			err:         sql.ErrNotFound,
		},
		{
			name:   "from > to",
			params: [3]uint32{23, 7, 5},
			err:    errBadRequest,
		},
		{
			name:   "by == 0",
			params: [3]uint32{7, 23, 0},
			err:    errBadRequest,
		},
		{
			name:   "count > 100",
			params: [3]uint32{7, 124, 1},
			err:    errBadRequest,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			th := createTestHandler(t)
			req := &MeshHashRequest{
				From: types.LayerID(tc.params[0]),
				To:   types.LayerID(tc.params[1]),
				Step: tc.params[2],
			}
			if !tc.hashMissing {
				for lid := req.From; !lid.After(req.To); lid = lid.Add(1) {
					require.NoError(t, layers.SetMeshHash(th.cdb, lid, types.RandomHash()))
				}
			}
			reqData, err := codec.Encode(req)
			require.NoError(t, err)

			resp, err := th.handleMeshHashReq(context.Background(), reqData)
			if tc.err == nil {
				require.NoError(t, err)
				got, err := codec.DecodeSlice[types.Hash32](resp)
				require.NoError(t, err)
				require.EqualValues(t, len(got), req.To.Difference(req.From)/req.Step+2)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}

func newAtx(t *testing.T, published types.EpochID) (*types.ActivationTx, types.AtxBlob) {
	t.Helper()
	nonce := uint64(123)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	atx := &wire.ActivationTxV1{
		InnerActivationTxV1: wire.InnerActivationTxV1{
			NIPostChallengeV1: wire.NIPostChallengeV1{
				PublishEpoch: published,
				PrevATXID:    types.RandomATXID(),
			},
			NumUnits: 2,
			VRFNonce: &nonce,
		},
	}
	atx.Sign(signer)
	return fixture.ToAtx(t, atx), atx.Blob()
}

func TestHandleEpochInfoReq(t *testing.T) {
	tt := []struct {
		name        string
		missingData bool
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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			th := createTestHandler(t)
			epoch := types.EpochID(11)
			var expected EpochData
			if !tc.missingData {
				for i := 0; i < 10; i++ {
					vatx, blob := newAtx(t, epoch)
					require.NoError(t, atxs.Add(th.cdb, vatx, blob))
					expected.AtxIDs = append(expected.AtxIDs, vatx.ID())
				}
			}

			epochBytes, err := codec.Encode(epoch)
			require.NoError(t, err)

			t.Run("non-streamed", func(t *testing.T) {
				out, err := th.handleEpochInfoReq(context.Background(), epochBytes)
				require.NoError(t, err)
				var got EpochData
				require.NoError(t, codec.Decode(out, &got))
				require.ElementsMatch(t, expected.AtxIDs, got.AtxIDs)
			})

			t.Run("streamed", func(t *testing.T) {
				var b bytes.Buffer
				require.NoError(t, th.handleEpochInfoReqStream(context.Background(), epochBytes, &b))
				var resp server.Response
				require.NoError(t, codec.Decode(b.Bytes(), &resp))
				var got EpochData
				require.NoError(t, codec.Decode(resp.Data, &got))
				require.ElementsMatch(t, expected.AtxIDs, got.AtxIDs)
			})

			t.Run("streamed request failure", func(t *testing.T) {
				th.db.Close()
				var b bytes.Buffer
				require.NoError(t, th.handleEpochInfoReqStream(context.Background(), epochBytes, &b))
				var resp server.Response
				require.NoError(t, codec.Decode(b.Bytes(), &resp))
				require.Empty(t, resp.Data)
				require.Contains(t, resp.Error, "exec epoch 11: database: no free connection")
			})
		})
	}
}

func testHandleEpochInfoReqWithQueryCache(
	t *testing.T,
	getInfo func(th *testHandler, req []byte, ed *EpochData),
) {
	th := createTestHandler(t, sql.WithQueryCache(true))
	require.True(t, th.cdb.Database.IsCached())
	require.True(t, sql.IsCached(th.cdb))
	epoch := types.EpochID(11)
	var expected EpochData

	for i := 0; i < 10; i++ {
		vatx, blob := newAtx(t, epoch)
		require.NoError(t, atxs.Add(th.cdb, vatx, blob))
		atxs.AtxAdded(th.cdb, vatx)
		expected.AtxIDs = append(expected.AtxIDs, vatx.ID())
	}

	require.Equal(t, 20, th.cdb.Database.QueryCount())
	epochBytes, err := codec.Encode(epoch)
	require.NoError(t, err)

	var got EpochData
	for i := 0; i < 3; i++ {
		getInfo(th, epochBytes, &got)
		require.ElementsMatch(t, expected.AtxIDs, got.AtxIDs)
		require.Equal(t, 21, th.cdb.Database.QueryCount(), "query count @ i = %d", i)
	}

	// Add another ATX which should be appended to the cached slice
	vatx, blob := newAtx(t, epoch)
	require.NoError(t, atxs.Add(th.cdb, vatx, blob))
	atxs.AtxAdded(th.cdb, vatx)
	expected.AtxIDs = append(expected.AtxIDs, vatx.ID())
	require.Equal(t, 23, th.cdb.Database.QueryCount())

	getInfo(th, epochBytes, &got)
	require.ElementsMatch(t, expected.AtxIDs, got.AtxIDs)
	// The query count is not incremented as the slice is still
	// cached and the new atx is just appended to it, even though
	// the response is re-serialized.
	require.Equal(t, 23, th.cdb.Database.QueryCount())
}

func TestHandleEpochInfoReqWithQueryCache(t *testing.T) {
	testHandleEpochInfoReqWithQueryCache(t, func(th *testHandler, req []byte, ed *EpochData) {
		out, err := th.handleEpochInfoReq(context.Background(), req)
		require.NoError(t, err)
		require.NoError(t, codec.Decode(out, ed))
	})
}

func TestHandleEpochInfoReqStreamWithQueryCache(t *testing.T) {
	testHandleEpochInfoReqWithQueryCache(t, func(th *testHandler, req []byte, ed *EpochData) {
		var b bytes.Buffer
		err := th.handleEpochInfoReqStream(context.Background(), req, &b)
		require.NoError(t, err)
		n, err := server.ReadResponse(&b, func(resLen uint32) (int, error) {
			return codec.DecodeFrom(&b, ed)
		})
		require.NoError(t, err)
		require.NotZero(t, n)
	})
}

func TestHandleMaliciousIDsReq(t *testing.T) {
	tt := []struct {
		name   string
		numBad int
	}{
		{
			name:   "some bad guys",
			numBad: 11,
		},
		{
			name: "no bad guys",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			th := createTestHandler(t)
			var bad []types.NodeID
			for i := 0; i < tc.numBad; i++ {
				nid := types.NodeID{byte(i + 1)}
				bad = append(bad, nid)
				require.NoError(t, identities.SetMalicious(th.cdb, nid, types.RandomBytes(11), time.Now()))
			}

			out, err := th.handleMaliciousIDsReq(context.TODO(), []byte{})
			require.NoError(t, err)
			var got MaliciousIDs
			require.NoError(t, codec.Decode(out, &got))
			require.ElementsMatch(t, bad, got.NodeIDs)
		})
	}
}
