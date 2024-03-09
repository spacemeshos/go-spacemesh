package localsql

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/natefinch/atomic"
	"github.com/spacemeshos/post/initialization"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func New0003Migration(log *zap.Logger, dataDir string, poetClients []PoetClient) *migration0003 {
	return &migration0003{
		logger:      log,
		dataDir:     dataDir,
		poetClients: poetClients,
	}
}

type migration0003 struct {
	logger      *zap.Logger
	dataDir     string
	poetClients []PoetClient
}

func (migration0003) Name() string {
	return "add nipost builder state"
}

func (migration0003) Order() int {
	return 3
}

func (m migration0003) Rollback() error {
	filename := filepath.Join(m.dataDir, builderFilename)
	backupName := fmt.Sprintf("%s.bak", filename)
	if err := atomic.ReplaceFile(backupName, filename); err != nil {
		return fmt.Errorf("rolling back nipost builder state: %w", err)
	}
	return nil
}

func (m migration0003) Apply(db sql.Executor) error {
	_, err := db.Exec("ALTER TABLE nipost RENAME TO challenge;", nil, nil)
	if err != nil {
		return fmt.Errorf("rename nipost table: %w", err)
	}

	_, err = db.Exec("ALTER TABLE challenge ADD COLUMN poet_proof_ref CHAR(32);", nil, nil)
	if err != nil {
		return fmt.Errorf("add poet_proof_ref column to challenge table: %w", err)
	}

	_, err = db.Exec("ALTER TABLE challenge ADD COLUMN poet_proof_membership VARCHAR;", nil, nil)
	if err != nil {
		return fmt.Errorf("add poet_proof_membership column to challenge table: %w", err)
	}

	if _, err := db.Exec(`CREATE TABLE poet_registration (
	    id            CHAR(32) NOT NULL,
	    hash          CHAR(32) NOT NULL,
	    address       VARCHAR NOT NULL,
	    round_id      VARCHAR NOT NULL,
	    round_end     INT NOT NULL,

	    PRIMARY KEY (id, address)
	) WITHOUT ROWID;`, nil, nil); err != nil {
		return fmt.Errorf("create poet_registration table: %w", err)
	}

	if _, err := db.Exec(`CREATE TABLE nipost (
		id            CHAR(32) PRIMARY KEY,
		post_nonce    UNSIGNED INT NOT NULL,
		post_indices  VARCHAR NOT NULL,
		post_pow      UNSIGNED LONG INT NOT NULL,

		num_units UNSIGNED INT NOT NULL,
		vrf_nonce UNSIGNED LONG INT NOT NULL,

		poet_proof_membership VARCHAR NOT NULL,
		poet_proof_ref        CHAR(32) NOT NULL,
		labels_per_unit       UNSIGNED INT NOT NULL
	) WITHOUT ROWID;`, nil, nil); err != nil {
		return fmt.Errorf("create nipost table: %w", err)
	}

	return m.moveNipostStateToDb(db, m.dataDir)
}

func (m migration0003) moveNipostStateToDb(db sql.Executor, dataDir string) error {
	state, err := loadBuilderState(dataDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil // no state to move
	case err != nil:
		return fmt.Errorf("load nipost builder state: %w", err)
	default:
	}

	meta, err := initialization.LoadMetadata(dataDir)
	if err != nil {
		return fmt.Errorf("load post metadata: %w", err)
	}

	if len(state.PoetRequests) == 0 {
		return discardBuilderState(dataDir) // Phase 0: Submit PoET challenge to PoET services
	}
	// Phase 0 completed.
	for _, req := range state.PoetRequests {
		address, err := m.getAddress(req.PoetServiceID)
		if err != nil {
			m.logger.Warn("failed to resolve address for poet service id during migration - skipping this PoET",
				zap.Binary("service_id", req.PoetServiceID.ServiceID),
				zap.Error(err),
			)
			continue
		}

		enc := func(stmt *sql.Statement) {
			stmt.BindBytes(1, meta.NodeId)
			stmt.BindBytes(2, state.Challenge.Bytes())
			stmt.BindText(3, address)
			stmt.BindText(4, req.PoetRound.ID)
			stmt.BindInt64(5, req.PoetRound.End.IntoTime().Unix())
		}
		if _, err := db.Exec(`
			insert into poet_registration (id, hash, address, round_id, round_end)
			values (?1, ?2, ?3, ?4, ?5);`, enc, nil,
		); err != nil {
			return fmt.Errorf("insert poet registration for %s: %w", types.BytesToNodeID(meta.NodeId).ShortString(), err)
		}
	}

	if state.PoetProofRef == types.EmptyPoetProofRef {
		// Phase 1: query PoET services for proof
		return discardBuilderState(dataDir)
	}
	// Phase 1 completed.
	buf, err := codec.Encode(&state.NIPost.Membership)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, meta.NodeId)
		stmt.BindBytes(2, state.PoetProofRef[:])
		stmt.BindBytes(3, buf)
	}
	rows, err := db.Exec(`
		update challenge set poet_proof_ref = ?2, poet_proof_membership = ?3
		where id = ?1 returning id;`, enc, nil)
	if err != nil {
		return fmt.Errorf("set poet proof ref for node id %s: %w", types.BytesToNodeID(meta.NodeId).ShortString(), err)
	}
	if rows == 0 {
		return fmt.Errorf("set poet proof ref for node id %s: %w",
			types.BytesToNodeID(meta.NodeId).ShortString(), sql.ErrNotFound,
		)
	}

	if state.NIPost.Post == nil {
		return discardBuilderState(dataDir) // Phase 2: Post execution
	}
	// Phase 2 completed.
	buf, err = codec.Encode(&state.NIPost.Membership)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	enc = func(stmt *sql.Statement) {
		stmt.BindBytes(1, meta.NodeId)
		stmt.BindInt64(2, int64(state.NIPost.Post.Nonce))
		stmt.BindBytes(3, state.NIPost.Post.Indices)
		stmt.BindInt64(4, int64(state.NIPost.Post.Pow))

		stmt.BindInt64(5, int64(meta.NumUnits))
		stmt.BindInt64(6, int64(*meta.Nonce))

		stmt.BindBytes(7, buf)

		stmt.BindBytes(8, state.NIPost.PostMetadata.Challenge)
		stmt.BindInt64(9, int64(state.NIPost.PostMetadata.LabelsPerUnit))
	}
	if _, err := db.Exec(`
		insert into nipost (id, post_nonce, post_indices, post_pow, num_units, vrf_nonce,
			 poet_proof_membership, poet_proof_ref, labels_per_unit
		) values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9);`, enc, nil,
	); err != nil {
		return fmt.Errorf("insert nipost for %s: %w", types.BytesToNodeID(meta.NodeId).ShortString(), err)
	}

	return discardBuilderState(dataDir)
}

func (m migration0003) getAddress(serviceID PoetServiceID) (string, error) {
	for _, client := range m.poetClients {
		clientId := client.PoetServiceID(context.Background())
		if bytes.Equal(serviceID.ServiceID, clientId) {
			return client.Address(), nil
		}
	}
	return "", fmt.Errorf("no poet client found for service id %x", serviceID.ServiceID)
}
