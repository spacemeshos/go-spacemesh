package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

const (
	SchemaVersion = "https://spacemesh.io/checkpoint.schema.json.1.0"

	checkpointDir = "checkpoint"
	schemaFile    = "schema.json"
	dirPerm       = 0o700
)

type Runner struct {
	fs     afero.Fs
	db     *sql.Database
	logger log.Log
	dir    string
}

type Opt func(*Runner)

func WithDataDir(dir string) Opt {
	return func(r *Runner) {
		r.dir = dir
	}
}

func WithLogger(lg log.Log) Opt {
	return func(r *Runner) {
		r.logger = lg
	}
}

func WithFilesystem(fs afero.Fs) Opt {
	return func(r *Runner) {
		r.fs = fs
	}
}

func NewRunner(db *sql.Database, opts ...Opt) *Runner {
	r := &Runner{
		fs:     afero.NewOsFs(),
		db:     db,
		logger: log.NewNop(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func checkpointDB(ctx context.Context, db *sql.Database, snapshot, restore types.LayerID) (*Checkpoint, error) {
	checkpoint := &Checkpoint{
		Version: SchemaVersion,
		Data: InnerData{
			CheckpointId: fmt.Sprintf("snapshot-%d-restore-%d", snapshot, restore),
			Restore:      restore.Uint32(),
		},
	}

	tx, err := db.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("create db tx: %s", err)
	}
	defer tx.Release()

	atxSnapshot, err := atxs.LatestN(tx, 2)
	if err != nil {
		return nil, fmt.Errorf("atxs snapshot: %w", err)
	}
	for i, catx := range atxSnapshot {
		commitmentAtx, err := atxs.CommitmentATX(tx, catx.SmesherID)
		if err != nil {
			return nil, fmt.Errorf("atxs snapshot commitment: %w", err)
		}
		vrfNonce, err := atxs.VRFNonce(tx, catx.SmesherID, snapshot.GetEpoch())
		if err != nil {
			return nil, fmt.Errorf("atxs snapshot nonce: %w", err)
		}
		copy(atxSnapshot[i].CommitmentATX[:], commitmentAtx[:])
		atxSnapshot[i].VRFNonce = vrfNonce
	}
	for _, catx := range atxSnapshot {
		checkpoint.Data.Atxs = append(checkpoint.Data.Atxs, ShortAtx{
			ID:             catx.ID.Bytes(),
			Epoch:          catx.Epoch.Uint32(),
			CommitmentAtx:  catx.CommitmentATX.Bytes(),
			VrfNonce:       uint64(catx.VRFNonce),
			NumUnits:       catx.NumUnits,
			BaseTickHeight: catx.BaseTickHeight,
			TickCount:      catx.TickCount,
			PublicKey:      catx.SmesherID.Bytes(),
			Sequence:       catx.Sequence,
			Coinbase:       catx.Coinbase.Bytes(),
		})
	}

	acctSnapshot, err := accounts.Snapshot(tx, snapshot)
	if err != nil {
		return nil, fmt.Errorf("accounts snapshot: %w", err)
	}
	for _, acct := range acctSnapshot {
		a := Account{
			Address: acct.Address.Bytes(),
			Balance: acct.Balance,
			Nonce:   acct.NextNonce,
		}
		if acct.TemplateAddress != nil {
			a.Template = acct.TemplateAddress.Bytes()
		}
		if acct.State != nil {
			a.State = acct.State
		}
		checkpoint.Data.Accounts = append(checkpoint.Data.Accounts, a)
	}
	return checkpoint, nil
}

func (r *Runner) Generate(ctx context.Context, snapshot, restore types.LayerID) (string, error) {
	checkpoint, err := checkpointDB(ctx, r.db, snapshot, restore)
	if err != nil {
		return "", err
	}
	rf, err := NewRecoveryFile(r.fs, SelfCheckpointFilename(r.dir, snapshot, restore))
	if err != nil {
		return "", fmt.Errorf("new recovery file: %w", err)
	}
	// one writer persist the checkpoint data, one returning result to caller.
	if err = json.NewEncoder(rf.fwriter).Encode(checkpoint); err != nil {
		return "", fmt.Errorf("marshal checkpoint json: %w", err)
	}
	if err = rf.save(r.fs); err != nil {
		return "", err
	}
	r.logger.With().Info("checkpoint persisted",
		log.String("checkpoint", rf.path),
		log.Int("size", rf.fwriter.Size()),
	)
	return rf.path, nil
}

func SelfCheckpointFilename(dataDir string, snapshotLayer, restoreLayer types.LayerID) string {
	return filepath.Join(filepath.Join(dataDir, checkpointDir), fmt.Sprintf("snapshot-%d-restore-%d", snapshotLayer, restoreLayer))
}
