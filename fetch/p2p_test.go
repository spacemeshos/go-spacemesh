package fetch

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

type blobKey struct {
	kind string
	id   types.Hash32
}

type testP2PFetch struct {
	t        *testing.T
	clientDB *sql.Database
	// client proposals
	clientPDB   *store.Store
	clientCDB   *datastore.CachedDB
	clientFetch *Fetch
	serverID    peer.ID
	serverDB    *sql.Database
	// server proposals
	serverPDB    *store.Store
	serverCDB    *datastore.CachedDB
	serverFetch  *Fetch
	recvMtx      sync.Mutex
	receivedData map[blobKey][]byte
}

func mkFakeValidator(tpf *testP2PFetch, kind string) SyncValidator {
	return ValidatorFunc(func(_ context.Context, id types.Hash32, _ peer.ID, data []byte) error {
		tpf.recvMtx.Lock()
		defer tpf.recvMtx.Unlock()
		k := blobKey{kind: kind, id: id}
		require.NotContains(tpf.t, tpf.receivedData, k)
		tpf.receivedData[k] = slices.Clone(data)
		return nil
	})
}

func p2pFetchCfg(streaming bool) Config {
	cfg := DefaultConfig()
	cfg.RequestTimeout = 3 * time.Second
	cfg.RequestHardTimeout = 10 * time.Second
	cfg.MaxRetriesForRequest = 3
	cfg.Streaming = streaming
	cfg.EnableServerMetrics = true
	return cfg
}

func p2pCfg(t *testing.T) p2p.Config {
	p2pconf := p2p.DefaultConfig()
	p2pconf.Listen = p2p.MustParseAddresses("/ip4/127.0.0.1/tcp/0")
	p2pconf.IP4Blocklist = nil
	p2pconf.DataDir = t.TempDir()
	return p2pconf
}

func createP2PFetch(
	t *testing.T,
	clientStreaming,
	serverStreaming,
	sqlCache bool,
	opts ...Option,
) (*testP2PFetch, context.Context) {
	lg := logtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	t.Cleanup(cancel)

	serverHost, err := p2p.New(ctx, lg, p2pCfg(t), []byte{}, []byte{})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, serverHost.Stop()) })

	clientHost, err := p2p.New(ctx, lg, p2pCfg(t), []byte{}, []byte{})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, clientHost.Stop()) })

	// TODO: connect after both NewFetch calls
	err = clientHost.Connect(ctx, peer.AddrInfo{
		ID:    serverHost.ID(),
		Addrs: serverHost.Addrs(),
	})
	require.NoError(t, err)

	require.Equal(t, 1, len(clientHost.GetPeers()))

	var sqlOpts []sql.Opt
	if sqlCache {
		sqlOpts = []sql.Opt{sql.WithQueryCache(true)}
	}
	clientDB := sql.InMemory(sqlOpts...)
	serverDB := sql.InMemory(sqlOpts...)
	tpf := &testP2PFetch{
		t:            t,
		clientDB:     clientDB,
		clientPDB:    store.New(store.WithLogger(lg.Zap())),
		clientCDB:    datastore.NewCachedDB(clientDB, lg),
		serverID:     serverHost.ID(),
		serverDB:     serverDB,
		serverPDB:    store.New(store.WithLogger(lg.Zap())),
		serverCDB:    datastore.NewCachedDB(serverDB, lg),
		receivedData: make(map[blobKey][]byte),
	}

	tpf.serverFetch = NewFetch(tpf.serverCDB, tpf.serverPDB, serverHost,
		WithContext(ctx),
		WithConfig(p2pFetchCfg(serverStreaming)),
		WithLogger(lg))
	vf := ValidatorFunc(
		func(context.Context, types.Hash32, peer.ID, []byte) error { return nil },
	)
	tpf.serverFetch.SetValidators(vf, vf, vf, vf, vf, vf, vf, vf, vf)
	require.NoError(t, tpf.serverFetch.Start())
	t.Cleanup(tpf.serverFetch.Stop)

	require.Eventually(t, func() bool {
		return len(serverHost.Mux().Protocols()) != 0
	}, 10*time.Second, 10*time.Millisecond)

	tpf.clientFetch = NewFetch(tpf.clientCDB, tpf.clientPDB, clientHost,
		WithContext(ctx),
		WithConfig(p2pFetchCfg(clientStreaming)),
		WithLogger(lg))
	tpf.clientFetch.SetValidators(
		mkFakeValidator(tpf, "atx"),
		mkFakeValidator(tpf, "poet"),
		mkFakeValidator(tpf, "ballot"),
		mkFakeValidator(tpf, "activeset"),
		mkFakeValidator(tpf, "block"),
		mkFakeValidator(tpf, "prop"),
		mkFakeValidator(tpf, "txBlock"),
		mkFakeValidator(tpf, "txProposal"),
		mkFakeValidator(tpf, "mal"),
	)
	require.NoError(t, tpf.clientFetch.Start())
	t.Cleanup(tpf.clientFetch.Stop)

	return tpf, ctx
}

func (tpf *testP2PFetch) createATXs(epoch types.EpochID) []types.ATXID {
	atxIDs := make([]types.ATXID, 10)
	for i := range atxIDs {
		atx := newAtx(tpf.t, epoch)
		require.NoError(tpf.t, atxs.Add(tpf.serverCDB, atx))
		atxIDs[i] = atx.ID()
	}
	return atxIDs
}

func (tpf *testP2PFetch) verifyGetHash(
	toCall func() error,
	errStr, kind, protocol string,
	h types.Hash32,
	id []byte,
	data []byte,
) {
	srv := tpf.serverFetch.servers[protocol].(*server.Server)
	numAccepted := srv.NumAcceptedRequests()
	if errStr != "" {
		tpf.serverDB.Close()
	}
	err := toCall()
	if errStr == "" {
		require.NoError(tpf.t, err)
		k := blobKey{kind: kind, id: h}
		require.Contains(tpf.t, tpf.receivedData, k)
		require.Equal(tpf.t, data, tpf.receivedData[k])
		require.Equal(tpf.t, numAccepted+1, srv.NumAcceptedRequests())
	} else {
		require.ErrorContains(tpf.t, err, errStr)
	}
}

func forStreaming(
	t *testing.T,
	errStr string,
	sqlCache bool,
	toCall func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string),
) {
	for _, tc := range []struct {
		name            string
		clientStreaming bool
		serverStreaming bool
	}{
		{
			name: "no streaming",
		},
		{
			name:            "client streaming",
			clientStreaming: true,
		},
		{
			name:            "server streaming",
			serverStreaming: true,
		},
		{
			name:            "client+server streaming",
			clientStreaming: true,
			serverStreaming: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("ok", func(t *testing.T) {
				tpf, ctx := createP2PFetch(t, tc.clientStreaming, tc.serverStreaming, sqlCache)
				toCall(t, ctx, tpf, "")
			})
			t.Run("fail", func(t *testing.T) {
				tpf, ctx := createP2PFetch(t, tc.clientStreaming, tc.serverStreaming, sqlCache)
				toCall(t, ctx, tpf, errStr)
			})
		})
	}
}

func forStreamingCachedUncached(
	t *testing.T,
	errStr string,
	toCall func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string),
) {
	for _, tc := range []struct {
		name     string
		sqlCache bool
	}{
		{
			name:     "no sql caching",
			sqlCache: false,
		},
		{
			name:     "sql caching",
			sqlCache: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forStreaming(t, errStr, tc.sqlCache, toCall)
		})
	}
}

func TestP2PPeerEpochInfo(t *testing.T) {
	forStreamingCachedUncached(
		t, "peer error: exec epoch 11: database: no free connection",
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			epoch := types.EpochID(11)
			atxIDs := tpf.createATXs(epoch)

			if errStr != "" {
				tpf.serverDB.Close()
			}

			got, err := tpf.clientFetch.PeerEpochInfo(context.Background(), tpf.serverID, epoch)
			if errStr == "" {
				require.NoError(t, err)
				require.ElementsMatch(t, atxIDs, got.AtxIDs)
			} else {
				require.ErrorContains(t, err, errStr)
			}
		})
}

func TestP2PPeerMeshHashes(t *testing.T) {
	forStreaming(
		t, "peer error: get aggHashes from 7 to 23 by 5: database: no free connection", false,
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			req := &MeshHashRequest{
				From: 7,
				To:   23,
				Step: 5,
			}
			var expected []types.Hash32
			for lid := req.From; !lid.After(req.To); lid = lid.Add(1) {
				hash := types.RandomHash()
				require.NoError(t, layers.SetMeshHash(tpf.serverCDB, lid, hash))
				if errStr == "" && (lid.Difference(req.From)%req.Step == 0 || lid == req.To) {
					expected = append(expected, hash)
				}
			}
			if errStr != "" {
				tpf.serverDB.Close()
			}

			mh, err := tpf.clientFetch.PeerMeshHashes(
				context.Background(), tpf.serverID, req)
			if errStr == "" {
				require.NoError(t, err)
				require.EqualValues(t, len(mh.Hashes), req.To.Difference(req.From)/req.Step+2)
				require.Equal(t, expected, mh.Hashes)
			} else {
				require.ErrorContains(t, err, errStr)
			}
		})
}

func TestP2PMaliciousIDs(t *testing.T) {
	forStreaming(
		t, "database: no free connection", false,
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			var bad []types.NodeID
			for i := 0; i < 11; i++ {
				nid := types.NodeID{byte(i + 1)}
				bad = append(bad, nid)
				require.NoError(t, identities.SetMalicious(
					tpf.serverCDB, nid, types.RandomBytes(11), time.Now()))
			}
			if errStr != "" {
				tpf.serverDB.Close()
			}

			out, err := tpf.clientFetch.GetMaliciousIDs(context.Background(), tpf.serverID)
			if errStr == "" {
				require.NoError(t, err)
				var got MaliciousIDs
				require.NoError(t, codec.Decode(out, &got))
				require.ElementsMatch(t, bad, got.NodeIDs)
			} else {
				require.ErrorContains(t, err, errStr)
			}
		})
}

func TestP2PGetATXs(t *testing.T) {
	forStreamingCachedUncached(
		t, "database: no free connection",
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			epoch := types.EpochID(11)
			atx := newAtx(tpf.t, epoch)
			require.NoError(tpf.t, atxs.Add(tpf.serverCDB, atx))
			tpf.verifyGetHash(
				func() error {
					return tpf.clientFetch.GetAtxs(
						context.Background(), []types.ATXID{atx.ID()})
				},
				errStr, "atx", "hs/1", types.Hash32(atx.ID()), atx.ID().Bytes(),
				codec.MustEncode(atx))
		})
}

func TestP2PGetPoet(t *testing.T) {
	forStreaming(
		t, "database: no free connection", false,
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			ref := types.PoetProofRef{0x42, 0x43}
			require.NoError(t, poets.Add(
				tpf.serverCDB, ref,
				[]byte("proof1"), []byte("sid1"), "rid1"))

			tpf.verifyGetHash(
				func() error {
					return tpf.clientFetch.GetPoetProof(
						context.Background(), types.Hash32(ref))
				},
				errStr, "poet", "hs/1", types.Hash32(ref), ref[:],
				[]byte("proof1"))
		})
}

func TestP2PGetBallot(t *testing.T) {
	forStreaming(
		t, "database: no free connection", false,
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			signer, err := signing.NewEdSigner()
			require.NoError(t, err)

			b := types.RandomBallot()
			b.Layer = types.LayerID(111)
			b.Signature = signer.Sign(signing.BALLOT, b.SignedBytes())
			b.SmesherID = signer.NodeID()
			require.NoError(t, b.Initialize())
			require.NoError(t, ballots.Add(tpf.serverCDB, b))

			tpf.verifyGetHash(
				func() error {
					return tpf.clientFetch.GetBallots(
						context.Background(), []types.BallotID{b.ID()})
				},
				errStr, "ballot", "hs/1", b.ID().AsHash32(), b.ID().Bytes(),
				codec.MustEncode(b))
		})
}

func TestP2PGetActiveSet(t *testing.T) {
	forStreamingCachedUncached(
		t, "database: no free connection",
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			id := types.RandomHash()
			set := &types.EpochActiveSet{
				Epoch: 2,
				Set:   []types.ATXID{{1}, {2}},
			}
			require.NoError(tpf.t, activesets.Add(tpf.serverCDB, id, set))

			tpf.verifyGetHash(
				func() error {
					return tpf.clientFetch.GetActiveSet(context.Background(), id)
				},
				errStr, "activeset", "as/1", id, id.Bytes(),
				codec.MustEncode(set))
		})
}

func TestP2PGetBlock(t *testing.T) {
	forStreaming(
		t, "database: no free connection", false,
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			lid := types.LayerID(111)
			bk := types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{LayerIndex: lid})
			require.NoError(t, blocks.Add(tpf.serverCDB, bk))

			tpf.verifyGetHash(
				func() error {
					return tpf.clientFetch.GetBlocks(
						context.Background(), []types.BlockID{bk.ID()})
				},
				errStr, "block", "hs/1", bk.ID().AsHash32(), bk.ID().Bytes(),
				codec.MustEncode(bk))
		})
}

func TestP2PGetProp(t *testing.T) {
	forStreaming(
		// TODO: it's probably doesn't make too much sense to retry if the hash is not found
		t, "failed after max retries", false,
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			nodeID := types.RandomNodeID()
			ballot := types.NewExistingBallot(types.BallotID{1}, types.RandomEdSignature(), nodeID, types.LayerID(0))
			require.NoError(t, ballots.Add(tpf.serverCDB, &ballot))
			proposal := &types.Proposal{
				InnerProposal: types.InnerProposal{
					Ballot:   ballot,
					TxIDs:    []types.TransactionID{{3, 4}},
					MeshHash: types.RandomHash(),
				},
				Signature: types.RandomEdSignature(),
			}
			proposal.SetID(types.ProposalID{7, 8})
			require.NoError(t, tpf.serverPDB.Add(proposal))
			tpf.verifyGetHash(
				func() error {
					id := proposal.ID()
					if errStr != "" {
						// Proposals are not fetched from SQLite DB
						// so simulating db error by closing it is not
						// going to work
						id = types.RandomProposalID()
					}
					return tpf.clientFetch.GetProposals(
						context.Background(), []types.ProposalID{id})
				},
				errStr, "prop", "hs/1", proposal.ID().AsHash32(), proposal.ID().Bytes(),
				codec.MustEncode(proposal))
		})
}

func TestP2PGetBlockTransactions(t *testing.T) {
	forStreaming(
		t, "database: no free connection", false,
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			signer, err := signing.NewEdSigner()
			require.NoError(t, err)
			tx := genTx(t, signer, types.Address{1}, 1, 1, 1)
			require.NoError(t, transactions.Add(tpf.serverCDB, &tx, time.Now()))
			tpf.verifyGetHash(
				func() error {
					return tpf.clientFetch.GetBlockTxs(
						context.Background(), []types.TransactionID{tx.ID})
				},
				errStr, "txBlock", "hs/1", types.Hash32(tx.ID), tx.ID.Bytes(),
				tx.Raw)
		})
}

func TestP2PGetProposalTransactions(t *testing.T) {
	forStreaming(
		t, "database: no free connection", false,
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			signer, err := signing.NewEdSigner()
			require.NoError(t, err)
			tx := genTx(t, signer, types.Address{1}, 1, 1, 1)
			require.NoError(t, transactions.Add(tpf.serverCDB, &tx, time.Now()))
			tpf.verifyGetHash(
				func() error {
					return tpf.clientFetch.GetProposalTxs(
						context.Background(), []types.TransactionID{tx.ID})
				},
				errStr, "txProposal", "hs/1", types.Hash32(tx.ID), tx.ID.Bytes(),
				tx.Raw)
		})
}

func TestP2PGetMalfeasanceProofs(t *testing.T) {
	forStreaming(
		t, "database: no free connection", false,
		func(t *testing.T, ctx context.Context, tpf *testP2PFetch, errStr string) {
			nid := types.RandomNodeID()
			proof := types.RandomBytes(11)
			require.NoError(t, identities.SetMalicious(
				tpf.serverCDB, nid, proof, time.Now()))
			tpf.verifyGetHash(
				func() error {
					return tpf.clientFetch.GetMalfeasanceProofs(
						context.Background(), []types.NodeID{nid})
				},
				errStr, "mal", "hs/1", types.Hash32(nid), nid.Bytes(),
				proof)
		})
}
