package checkpoint

import (
	"context"
	"encoding/hex"
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

func (r *Runner) Generate(ctx context.Context, snapshotLayer types.LayerID, restoreLayer types.LayerID) ([]byte, error) {
	checkpoint := Checkpoint{
		Version: SchemaVersion,
		Data: InnerData{
			CheckpointId: fmt.Sprintf("snapshot-%d-restore-%d", snapshotLayer, restoreLayer),
			Restore:      restoreLayer.Uint32(),
		},
	}

	var (
		atxSnapshot  []atxs.CheckpointAtx
		acctSnapshot []*types.Account
		err          error
	)
	if err = r.db.WithTx(ctx, func(tx *sql.Tx) error {
		atxSnapshot, err = atxs.LatestTwo(tx)
		if err != nil {
			return fmt.Errorf("atxs snapshot: %w", err)
		}
		for i, catx := range atxSnapshot {
			commitmentAtx, err := atxs.CommitmentATX(tx, catx.SmesherID)
			if err != nil {
				return fmt.Errorf("atxs snapshot commitment: %w", err)
			}
			r.logger.With().Info("found commitment atx",
				log.Stringer("smesher", catx.SmesherID),
				log.Stringer("commitmentATX", commitmentAtx),
			)
			vrfNonce, err := atxs.VRFNonce(tx, catx.SmesherID, snapshotLayer.GetEpoch())
			if err != nil {
				return fmt.Errorf("atxs snapshot nonce: %w", err)
			}
			r.logger.With().Info("found vrf nonce",
				log.Stringer("smesher", catx.SmesherID),
				log.Uint64("nonce", uint64(vrfNonce)),
			)
			copy(atxSnapshot[i].CommitmentATX[:], commitmentAtx[:])
			atxSnapshot[i].VRFNonce = vrfNonce
		}
		acctSnapshot, err = accounts.Snapshot(tx, snapshotLayer)
		if err != nil {
			return fmt.Errorf("accounts snapshot: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	for _, catx := range atxSnapshot {
		checkpoint.Data.Atxs = append(checkpoint.Data.Atxs, ShortAtx{
			ID:             hex.EncodeToString(catx.ID.Bytes()),
			Epoch:          catx.Epoch.Uint32(),
			CommitmentAtx:  hex.EncodeToString(catx.CommitmentATX.Bytes()),
			VrfNonce:       uint64(catx.VRFNonce),
			NumUnits:       catx.NumUnits,
			BaseTickHeight: catx.BaseTickHeight,
			TickCount:      catx.TickCount,
			PublicKey:      hex.EncodeToString(catx.SmesherID.Bytes()),
			Sequence:       catx.Sequence,
			Coinbase:       hex.EncodeToString(catx.Coinbase.Bytes()),
		})
	}
	for _, acct := range acctSnapshot {
		a := Account{
			Address: hex.EncodeToString(acct.Address.Bytes()),
			Balance: acct.Balance,
			Nonce:   acct.NextNonce,
			State:   hex.EncodeToString(acct.State),
		}
		if acct.TemplateAddress != nil {
			a.Template = hex.EncodeToString(acct.TemplateAddress.Bytes())
		}
		checkpoint.Data.Accounts = append(checkpoint.Data.Accounts, a)
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return nil, fmt.Errorf("marshal checkpoint json: %w", err)
	}
	if err = ValidateSchema(data); err != nil {
		return nil, fmt.Errorf("validate against schema: %w", err)
	}
	filename := SelfCheckpointFilename(r.dir, snapshotLayer, restoreLayer)
	if err = savefile(r.logger, r.fs, filename, data); err != nil {
		return nil, err
	}
	r.logger.With().Info("checkpoint persisted",
		log.String("checkpoint", filename),
		log.String("content", string(data)),
	)
	if _, err = r.fs.Stat(filename); err != nil {
		r.logger.Panic("failed to persist checkpoint data",
			log.String("checkpoint", filename),
			log.Err(err),
		)
	}
	return data, nil
}

func SelfCheckpointFilename(dataDir string, snapshotLayer, restoreLayer types.LayerID) string {
	return filepath.Join(filepath.Join(dataDir, checkpointDir), fmt.Sprintf("snapshot-%d-restore-%d", snapshotLayer, restoreLayer))
}
