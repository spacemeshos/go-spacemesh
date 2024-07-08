package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spf13/afero"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

const (
	SchemaVersion = "https://spacemesh.io/checkpoint.schema.json.1.0"

	CommandString = "grpcurl -plaintext -d '%s' 0.0.0.0:9093 spacemesh.v1.AdminService.CheckpointStream"

	checkpointDir = "checkpoint"
	schemaFile    = "schema.json"
	dirPerm       = 0o700
)

func checkpointDB(
	ctx context.Context,
	db sql.StateDatabase,
	snapshot types.LayerID,
	numAtxs int,
) (*types.Checkpoint, error) {
	request, err := json.Marshal(&pb.CheckpointStreamRequest{
		SnapshotLayer: uint32(snapshot),
		NumAtxs:       uint32(numAtxs),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	checkpoint := &types.Checkpoint{
		Command: fmt.Sprintf(CommandString, request),
		Version: SchemaVersion,
		Data: types.InnerData{
			CheckpointId: fmt.Sprintf("snapshot-%d", snapshot),
			Marriages:    make(map[types.ATXID][]types.MarriageSnaphot),
		},
	}

	tx, err := db.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("create db tx: %w", err)
	}
	defer tx.Release()

	atxSnapshot, err := atxs.LatestN(tx, numAtxs)
	if err != nil {
		return nil, fmt.Errorf("atxs snapshot: %w", err)
	}
	malicious := map[types.NodeID]bool{}
	for i, catx := range atxSnapshot {
		if _, ok := malicious[catx.SmesherID]; !ok {
			mal, err := identities.IsMalicious(tx, catx.SmesherID)
			if err != nil {
				return nil, fmt.Errorf("atxs snapshot check identity: %w", err)
			}
			malicious[catx.SmesherID] = mal
		}
		commitmentAtx, err := atxs.CommitmentATX(tx, catx.SmesherID)
		if err != nil {
			return nil, fmt.Errorf("atxs snapshot commitment: %w", err)
		}
		atxSnapshot[i].CommitmentATX = commitmentAtx
	}
	for _, catx := range atxSnapshot {
		if mal, ok := malicious[catx.SmesherID]; ok && mal {
			continue
		}
		checkpoint.Data.Atxs = append(checkpoint.Data.Atxs, types.AtxSnapshot{
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
			Units:          catx.Units,
		})
	}

	acctSnapshot, err := accounts.Snapshot(tx, snapshot)
	if err != nil {
		return nil, fmt.Errorf("accounts snapshot: %w", err)
	}
	for _, acct := range acctSnapshot {
		a := types.AccountSnapshot{
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
	err = identities.IterateMarriages(tx, func(id types.NodeID, data *identities.MarriageData) bool {
		checkpoint.Data.Marriages[data.ATX] = append(checkpoint.Data.Marriages[data.ATX], types.MarriageSnaphot{
			Index:     data.Index,
			Signer:    id.Bytes(),
			MarriedTo: data.Target.Bytes(),
			Signature: data.Signature.Bytes(),
		})
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("collecting marriages: %w", err)
	}

	// collect marriage ATXs
	for id := range checkpoint.Data.Marriages {
		atx, err := atxs.Get(tx, id)
		if err != nil {
			return nil, fmt.Errorf("getting marriage atx: %w", err)
		}
		snapshot := types.AtxSnapshot{
			ID:             id.Bytes(),
			Epoch:          atx.PublishEpoch.Uint32(),
			VrfNonce:       uint64(atx.VRFNonce),
			NumUnits:       atx.NumUnits,
			BaseTickHeight: atx.BaseTickHeight,
			TickCount:      atx.TickCount,
			PublicKey:      atx.SmesherID.Bytes(),
			Sequence:       atx.Sequence,
			Coinbase:       atx.Coinbase.Bytes(),
		}

		snapshot.Units, err = atxs.AllUnits(tx, id)
		if err != nil {
			return nil, fmt.Errorf("getting units for ATX %s: %w", id, err)
		}

		if atx.CommitmentATX != nil {
			snapshot.CommitmentAtx = atx.CommitmentATX.Bytes()
		} else {
			commitment, err := atxs.CommitmentATX(tx, atx.SmesherID)
			if err != nil {
				return nil, fmt.Errorf("getting commitment for smesher %s: %w", atx.SmesherID, err)
			}
			snapshot.CommitmentAtx = commitment.Bytes()
		}

		checkpoint.Data.Atxs = append(checkpoint.Data.Atxs, snapshot)
	}

	return checkpoint, nil
}

func Generate(
	ctx context.Context,
	fs afero.Fs,
	db sql.StateDatabase,
	dataDir string,
	snapshot types.LayerID,
	numAtxs int,
) error {
	checkpoint, err := checkpointDB(ctx, db, snapshot, numAtxs)
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
