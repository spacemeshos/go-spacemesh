package checkpoint_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacemeshos/poet/shared"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/recovery"
)

func atxequal(tb testing.TB, satx checkpoint.ShortAtx, vatx *types.VerifiedActivationTx, commitAtx types.ATXID, vrfnonce types.VRFPostIndex) {
	require.True(tb, bytes.Equal(satx.ID, vatx.ID().Bytes()))
	require.EqualValues(tb, satx.Epoch, vatx.PublishEpoch)
	require.True(tb, bytes.Equal(satx.CommitmentAtx, commitAtx.Bytes()))
	require.EqualValues(tb, satx.VrfNonce, vrfnonce)
	require.Equal(tb, satx.NumUnits, vatx.NumUnits)
	require.Equal(tb, satx.BaseTickHeight, vatx.BaseTickHeight())
	require.Equal(tb, satx.TickCount, vatx.TickCount())
	require.True(tb, bytes.Equal(satx.PublicKey, vatx.SmesherID.Bytes()))
	require.Equal(tb, satx.Sequence, vatx.Sequence)
	require.True(tb, bytes.Equal(satx.Coinbase, vatx.Coinbase.Bytes()))
	require.True(tb, vatx.Golden())
}

func accountequal(tb testing.TB, cacct checkpoint.Account, acct *types.Account) {
	require.True(tb, bytes.Equal(cacct.Address, acct.Address.Bytes()))
	require.Equal(tb, cacct.Balance, acct.Balance)
	require.Equal(tb, cacct.Nonce, acct.NextNonce)
	if cacct.Template == nil {
		require.Nil(tb, acct.TemplateAddress)
		require.Nil(tb, acct.State)
	} else {
		require.True(tb, bytes.Equal(cacct.Template, acct.TemplateAddress.Bytes()))
		require.True(tb, bytes.Equal(cacct.State, acct.State))
	}
}

func verifyDbContent(tb testing.TB, db *sql.Database) []*types.VerifiedActivationTx {
	var expected checkpoint.Checkpoint
	require.NoError(tb, json.Unmarshal([]byte(checkpointdata), &expected))
	expAtx := map[types.ATXID]checkpoint.ShortAtx{}
	for _, satx := range expected.Data.Atxs {
		expAtx[types.ATXID(types.BytesToHash(satx.ID))] = satx
	}
	expAcct := map[types.Address]checkpoint.Account{}
	for _, acct := range expected.Data.Accounts {
		var addr types.Address
		copy(addr[:], acct.Address)
		expAcct[addr] = acct
	}
	allIds, err := atxs.All(db)
	require.NoError(tb, err)
	var extra []*types.VerifiedActivationTx
	for _, id := range allIds {
		vatx, err := atxs.Get(db, id)
		require.NoError(tb, err)
		commitatx, err := atxs.CommitmentATX(db, vatx.SmesherID)
		require.NoError(tb, err)
		vrfnonce, err := atxs.VRFNonce(db, vatx.SmesherID, vatx.PublishEpoch+1)
		require.NoError(tb, err)
		if _, ok := expAtx[id]; ok {
			atxequal(tb, expAtx[id], vatx, commitatx, vrfnonce)
		} else {
			extra = append(extra, vatx)
		}
	}
	allAccts, err := accounts.All(db)
	require.NoError(tb, err)
	for _, acct := range allAccts {
		cacct, ok := expAcct[acct.Address]
		require.True(tb, ok)
		require.NotNil(tb, acct)
		accountequal(tb, cacct, acct)
	}
	return extra
}

func TestReadCheckpointAndDie(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/snapshot-15", ts.URL)
	dataDir := t.TempDir()
	require.Panics(t, func() { checkpoint.ReadCheckpointAndDie(ctx, logtest.New(t), dataDir, url, 18) })

	fname := checkpoint.RecoveryFilename(dataDir, filepath.Base(url), 18)
	got, err := os.ReadFile(fname)
	require.NoError(t, err)
	require.True(t, bytes.Equal([]byte(checkpointdata), got))
}

func TestRecoverFromHttp(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()

	tt := []struct {
		name string
		uri  string
		fail bool
	}{
		{
			name: "http",
			uri:  fmt.Sprintf("%s/snapshot-15", ts.URL),
		},
		{
			name: "ftp",
			uri:  "ftp://snapshot-15",
			fail: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			fs := afero.NewMemMapFs()
			cfg := &checkpoint.RecoverConfig{
				GoldenAtx: types.ATXID{1},
				DataDir:   t.TempDir(),
				DbFile:    "state.sql",
			}
			db := sql.InMemory()
			newdb, err := checkpoint.RecoverWithDb(ctx, logtest.New(t), db, fs, cfg, types.NodeID{2, 3, 4}, tc.uri, 18)
			if tc.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, newdb)
			exras := verifyDbContent(t, newdb)
			require.Empty(t, exras)
			restore, err := recovery.CheckpointInfo(newdb)
			require.NoError(t, err)
			require.EqualValues(t, 18, restore)
		})
	}
}

func TestRecoverSameRecoveryInfo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fs := afero.NewMemMapFs()
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx: types.ATXID{1},
		DataDir:   t.TempDir(),
		DbFile:    "state.sql",
	}
	url := fmt.Sprintf("%s/snapshot-15-restore-18", ts.URL)
	db := sql.InMemory()
	require.NoError(t, recovery.SetCheckpoint(db, 18))
	newdb, err := checkpoint.RecoverWithDb(ctx, logtest.New(t), db, fs, cfg, types.NodeID{2, 3, 4}, url, 18)
	require.NoError(t, err)
	require.Nil(t, newdb)
}

func TestRecover_OwnAtxNotInCheckpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := &checkpoint.RecoverConfig{
		GoldenAtx: types.ATXID{1},
		DataDir:   t.TempDir(),
		DbFile:    "test.sql",
	}

	olddb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	nid := types.NodeID{1, 2, 3}
	prev := newvatx(t, newatx(types.ATXID{12}, &types.ATXID{1}, 2, 0, 123, nid.Bytes()))
	require.NoError(t, atxs.Add(olddb, prev))
	atx := newatx(types.ATXID{13}, nil, 3, 1, 0, nid.Bytes())
	atx.PrevATXID = prev.ID()
	atx.PositioningATX = prev.ID()
	atx.NIPost = &types.NIPost{
		PostMetadata: &types.PostMetadata{
			Challenge: []byte{3, 4, 5},
		},
	}
	vatx := newvatx(t, atx)
	require.NoError(t, atxs.Add(olddb, vatx))
	proofMessage := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			MerkleProof: shared.MerkleProof{
				Root:         []byte{1, 2, 3},
				ProvenLeaves: [][]byte{{1}, {2}},
				ProofNodes:   [][]byte{{1}, {2}},
			},
			LeafCount: 1234,
		},
		PoetServiceID: []byte("poet_id_123456"),
		RoundID:       "1337",
	}
	encoded, err := codec.Encode(proofMessage)
	require.NoError(t, err)
	require.NoError(t, poets.Add(olddb, types.PoetProofRef(atx.GetPoetProofRef()), encoded, proofMessage.PoetServiceID, proofMessage.RoundID))

	url := fmt.Sprintf("%s/snapshot-15", ts.URL)
	require.NoError(t, checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg, nid, url, 18))

	newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)

	require.NoError(t, err)
	require.NotNil(t, newdb)
	extras := verifyDbContent(t, newdb)
	require.ElementsMatch(t, extras, []*types.VerifiedActivationTx{prev, vatx})
	gotProof, err := poets.Get(newdb, types.PoetProofRef(atx.GetPoetProofRef()))
	require.NoError(t, err)
	require.Equal(t, encoded, gotProof)
	restore, err := recovery.CheckpointInfo(newdb)
	require.NoError(t, err)
	require.EqualValues(t, 18, restore)

	// sqlite create .sql, .sql-shm and .sql-wal files.
	files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.Greater(t, len(files), 1)
}

func TestRecover(t *testing.T) {
	tt := []struct {
		name                string
		fname, dataDir, dir string
	}{
		{
			name:    "from local file",
			dataDir: t.TempDir(),
			dir:     "checkpoint",
			fname:   "snapshot-15",
		},
		{
			name:    "from recovery file",
			dataDir: t.TempDir(),
			dir:     "recovery",
			fname:   "snapshot-15-restore-18",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			fs := afero.NewMemMapFs()
			src := filepath.Join(tc.dataDir, tc.dir, tc.fname)
			require.NoError(t, afero.WriteFile(fs, src, []byte(checkpointdata), 0o600))
			cfg := &checkpoint.RecoverConfig{
				GoldenAtx: types.ATXID{1},
				DataDir:   tc.dataDir,
				DbFile:    "test.sql",
			}

			uri := fmt.Sprintf("file://%s", src)
			require.NoError(t, checkpoint.Recover(ctx, logtest.New(t), fs, cfg, types.NodeID{2, 3, 4}, uri, 18))

			db, err := sql.Open("file:" + filepath.Join(tc.dataDir, cfg.DbFile))
			require.NoError(t, err)
			require.NotNil(t, db)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			extras := verifyDbContent(t, db)
			require.Empty(t, extras)
			restore, err := recovery.CheckpointInfo(db)
			require.NoError(t, err)
			require.EqualValues(t, 18, restore)
		})
	}
}

func TestParseRestoreLayer(t *testing.T) {
	tt := []struct {
		name  string
		fname string
		exp   types.LayerID
	}{
		{
			name:  "good",
			fname: "snapshot-15-restore-18",
			exp:   types.LayerID(18),
		},
		{
			name:  "no restore",
			fname: "snapshot-18",
			exp:   types.LayerID(0),
		},
		{
			name:  "snapshot later than restore",
			fname: "snapshot-18-restore-15",
			exp:   types.LayerID(0),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			restore, err := checkpoint.ParseRestoreLayer(filepath.Join(checkpoint.RecoveryDir(t.TempDir()), tc.fname))
			if tc.exp == 0 {
				require.Error(t, err)
			} else {
				require.Equal(t, tc.exp, restore)
			}
		})
	}
}
