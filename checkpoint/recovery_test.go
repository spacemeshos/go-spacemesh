package checkpoint_test

import (
	"bytes"
	"context"
	"encoding/hex"
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

	"github.com/spacemeshos/go-spacemesh/bootstrap"
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

const recoverLayer uint32 = 18

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
		require.EqualValues(tb, recoverLayer-1, acct.Layer)
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

	tt := []struct {
		name   string
		uri    string
		expErr string
	}{
		{
			name: "all good",
			uri:  fmt.Sprintf("%s/snapshot-15", ts.URL),
		},
		{
			name:   "file does not exist",
			uri:    "file:///snapshot-15",
			expErr: "no such file or directory",
		},
		{
			name:   "invalid uri",
			uri:    fmt.Sprintf("%s^/snapshot-15", ts.URL),
			expErr: "parse recovery URI",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			dataDir := t.TempDir()
			if len(tc.expErr) > 0 {
				err := checkpoint.ReadCheckpointAndDie(ctx, logtest.New(t), dataDir, tc.uri, types.LayerID(recoverLayer))
				require.ErrorContains(t, err, tc.expErr)
			} else {
				require.Panics(t, func() {
					checkpoint.ReadCheckpointAndDie(ctx, logtest.New(t), dataDir, tc.uri, types.LayerID(recoverLayer))
				})
			}

			fname := checkpoint.RecoveryFilename(dataDir, filepath.Base(tc.uri), types.LayerID(recoverLayer))
			got, err := os.ReadFile(fname)
			if len(tc.expErr) > 0 {
				require.ErrorIs(t, err, os.ErrNotExist)
			} else {
				require.NoError(t, err)
				require.True(t, bytes.Equal([]byte(checkpointdata), got))
			}
		})
	}
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
		name   string
		uri    string
		expErr string
	}{
		{
			name: "http",
			uri:  fmt.Sprintf("%s/snapshot-15", ts.URL),
		},
		{
			name:   "url unreachable",
			uri:    "http://nowhere/snapshot-15",
			expErr: "dial tcp: lookup nowhere",
		},
		{
			name:   "ftp",
			uri:    "ftp://snapshot-15",
			expErr: "uri scheme not supported",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			fs := afero.NewMemMapFs()
			cfg := &checkpoint.RecoverConfig{
				GoldenAtx:      types.ATXID{1},
				DataDir:        t.TempDir(),
				DbFile:         "test.sql",
				PreserveOwnAtx: true,
			}
			bsdir := filepath.Join(cfg.DataDir, bootstrap.DirName)
			require.NoError(t, fs.MkdirAll(bsdir, 0o700))
			db := sql.InMemory()
			newdb, err := checkpoint.RecoverWithDb(ctx, logtest.New(t), db, fs, cfg, types.NodeID{2, 3, 4}, tc.uri, types.LayerID(recoverLayer))
			if len(tc.expErr) > 0 {
				require.ErrorContains(t, err, tc.expErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, newdb)
			exras := verifyDbContent(t, newdb)
			require.Empty(t, exras)
			restore, err := recovery.CheckpointInfo(newdb)
			require.NoError(t, err)
			require.EqualValues(t, recoverLayer, restore)
			exist, err := afero.Exists(fs, bsdir)
			require.NoError(t, err)
			require.False(t, exist)
		})
	}
}

func TestRecover_SameRecoveryInfo(t *testing.T) {
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
		GoldenAtx:      types.ATXID{1},
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		PreserveOwnAtx: true,
	}
	bsdir := filepath.Join(cfg.DataDir, bootstrap.DirName)
	require.NoError(t, fs.MkdirAll(bsdir, 0o700))
	url := fmt.Sprintf("%s/snapshot-15", ts.URL)
	db := sql.InMemory()
	types.SetEffectiveGenesis(0)
	require.NoError(t, recovery.SetCheckpoint(db, types.LayerID(recoverLayer)))
	newdb, err := checkpoint.RecoverWithDb(ctx, logtest.New(t), db, fs, cfg, types.NodeID{2, 3, 4}, url, types.LayerID(recoverLayer))
	require.NoError(t, err)
	require.Nil(t, newdb)
	require.EqualValues(t, recoverLayer-1, types.GetEffectiveGenesis())
	exist, err := afero.Exists(fs, bsdir)
	require.NoError(t, err)
	require.True(t, exist)
}

func TestRecover_OwnAtxNotInCheckpoint(t *testing.T) {
	tt := []struct {
		name     string
		preserve bool
	}{
		{
			name:     "preserve own atx",
			preserve: true,
		},
		{
			name: "do not preserve own atx",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
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
				GoldenAtx:      types.ATXID{1},
				DataDir:        t.TempDir(),
				DbFile:         "test.sql",
				PreserveOwnAtx: tc.preserve,
			}

			olddb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
			require.NoError(t, err)
			require.NotNil(t, olddb)
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
			require.NoError(t, olddb.Close())

			url := fmt.Sprintf("%s/snapshot-15", ts.URL)
			require.NoError(t, checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg, nid, url, types.LayerID(recoverLayer)))

			newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
			require.NoError(t, err)
			require.NotNil(t, newdb)
			t.Cleanup(func() { require.NoError(t, newdb.Close()) })
			extras := verifyDbContent(t, newdb)
			if tc.preserve {
				require.ElementsMatch(t, extras, []*types.VerifiedActivationTx{prev, vatx})
				gotProof, err := poets.Get(newdb, types.PoetProofRef(atx.GetPoetProofRef()))
				require.NoError(t, err)
				require.Equal(t, encoded, gotProof)
			} else {
				require.Empty(t, extras)
			}
			restore, err := recovery.CheckpointInfo(newdb)
			require.NoError(t, err)
			require.EqualValues(t, recoverLayer, restore)

			// sqlite create .sql, .sql-shm and .sql-wal files.
			files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
			require.NoError(t, err)
			require.Greater(t, len(files), 1)
		})
	}
}

func TestRecover_OwnAtxInCheckpoint(t *testing.T) {
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
		GoldenAtx:      types.ATXID{1},
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		PreserveOwnAtx: true,
	}

	olddb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, olddb)
	data, err := hex.DecodeString("0230c5d75d42b84f98800eceb47bc9cc4d803058900a50346a09ff61d56b6582")
	require.NoError(t, err)
	nid := types.BytesToNodeID(data)
	data, err = hex.DecodeString("98e47278c1f58acfd2b670a730f28898f74eb140482a07b91ff81f9ff0b7d9f4")
	require.NoError(t, err)
	atx := newatx(types.ATXID(types.BytesToHash(data)), nil, 3, 1, 0, nid.Bytes())
	require.NoError(t, atxs.Add(olddb, newvatx(t, atx)))
	require.NoError(t, olddb.Close())

	url := fmt.Sprintf("%s/snapshot-15", ts.URL)
	require.NoError(t, checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg, nid, url, types.LayerID(recoverLayer)))

	newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newdb)
	t.Cleanup(func() { require.NoError(t, newdb.Close()) })
	extras := verifyDbContent(t, newdb)
	require.Empty(t, extras)
	restore, err := recovery.CheckpointInfo(newdb)
	require.NoError(t, err)
	require.EqualValues(t, recoverLayer, restore)

	// sqlite create .sql, .sql-shm and .sql-wal files.
	files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.Greater(t, len(files), 1)
}

func TestRecover(t *testing.T) {
	tt := []struct {
		name           string
		fname, oldFile string
		dataDir, dir   string
		missing, fail  bool
		invalidData    []byte
	}{
		{
			name:    "from local file",
			dataDir: t.TempDir(),
			dir:     "checkpoint",
			fname:   "snapshot-15",
		},
		{
			name:    "old recovery file",
			dataDir: t.TempDir(),
			dir:     "checkpoint",
			fname:   "snapshot-15",
			oldFile: "snapshot-10",
		},
		{
			name:    "file does not exist",
			dataDir: t.TempDir(),
			dir:     "checkpoint",
			fname:   "snapshot-15",
			missing: true,
			fail:    true,
		},
		{
			name:    "from recovery file",
			dataDir: t.TempDir(),
			dir:     "recovery",
			fname:   "snapshot-15-restore-18",
		},
		{
			name:        "invalid data",
			fname:       "snapshot-15-restore-18",
			dataDir:     t.TempDir(),
			dir:         "recovery",
			missing:     false,
			invalidData: []byte("invalid"),
			fail:        true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			fs := afero.NewMemMapFs()
			if tc.oldFile != "" {
				old := filepath.Join(checkpoint.RecoveryDir(tc.dataDir), tc.oldFile)
				require.NoError(t, afero.WriteFile(fs, old, []byte{1, 2, 3}, 0o600))
			}
			src := filepath.Join(tc.dataDir, tc.dir, tc.fname)
			if !tc.missing {
				if tc.invalidData != nil {
					require.NoError(t, afero.WriteFile(fs, src, tc.invalidData, 0o600))
				} else {
					require.NoError(t, afero.WriteFile(fs, src, []byte(checkpointdata), 0o600))
				}
			}
			cfg := &checkpoint.RecoverConfig{
				GoldenAtx:      types.ATXID{1},
				DataDir:        tc.dataDir,
				DbFile:         "test.sql",
				PreserveOwnAtx: true,
			}

			uri := fmt.Sprintf("file://%s", src)
			err := checkpoint.Recover(ctx, logtest.New(t), fs, cfg, types.NodeID{2, 3, 4}, uri, types.LayerID(recoverLayer))
			if tc.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			db, err := sql.Open("file:" + filepath.Join(tc.dataDir, cfg.DbFile))
			require.NoError(t, err)
			require.NotNil(t, db)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			extras := verifyDbContent(t, db)
			require.Empty(t, extras)
			restore, err := recovery.CheckpointInfo(db)
			require.NoError(t, err)
			require.EqualValues(t, recoverLayer, restore)

			files, err := afero.Glob(fs, fmt.Sprintf("%s*", checkpoint.RecoveryDir(tc.dataDir)))
			require.NoError(t, err)
			if tc.oldFile != "" {
				require.Len(t, files, 2)
			} else {
				require.Len(t, files, 1)
			}
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
