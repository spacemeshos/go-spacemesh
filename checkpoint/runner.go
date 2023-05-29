package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/spacemeshos/go-spacemesh/common/types"
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

func checkpointDB(ctx context.Context, db *sql.Database, snapshot types.LayerID) (*Checkpoint, error) {
	checkpoint := &Checkpoint{
		Version: SchemaVersion,
		Data: InnerData{
			CheckpointId: fmt.Sprintf("snapshot-%d", snapshot),
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

func Generate(ctx context.Context, fs afero.Fs, db *sql.Database, dataDir string, snapshot types.LayerID) error {
	checkpoint, err := checkpointDB(ctx, db, snapshot)
	if err != nil {
		return err
	}
	rf, err := NewRecoveryFile(fs, SelfCheckpointFilename(dataDir, snapshot))
	if err != nil {
		return fmt.Errorf("new recovery file: %w", err)
	}
	// one writer persist the checkpoint data, one returning result to caller.
	if err = json.NewEncoder(rf.fwriter).Encode(checkpoint); err != nil {
		return fmt.Errorf("marshal checkpoint json: %w", err)
	}
	if err = rf.Save(fs); err != nil {
		return err
	}
	return nil
}

func SelfCheckpointFilename(dataDir string, snapshot types.LayerID) string {
	return filepath.Join(filepath.Join(dataDir, checkpointDir), fmt.Sprintf("snapshot-%d", snapshot))
}
