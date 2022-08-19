package layers

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	hashField           = "hash"
	aggregatedHashField = "aggregated_hash"
)

// SetWeakCoin for the layer.
func SetWeakCoin(db sql.Executor, lid types.LayerID, weakcoin bool) error {
	if _, err := db.Exec(`insert into layers (id, weak_coin) values (?1, ?2) 
					on conflict(id) do update set weak_coin=?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBool(2, weakcoin)
		}, nil); err != nil {
		return fmt.Errorf("set weak coin %s: %w", lid, err)
	}
	return nil
}

// GetWeakCoin for layer.
func GetWeakCoin(db sql.Executor, lid types.LayerID) (bool, error) {
	var (
		weakcoin bool
		err      error
		rows     int
	)
	if rows, err = db.Exec("select weak_coin from layers where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
		},
		func(stmt *sql.Statement) bool {
			if stmt.ColumnLen(0) == 0 {
				err = fmt.Errorf("%w weak coin for %s is null", sql.ErrNotFound, lid)
				return false
			}
			weakcoin = stmt.ColumnInt(0) == 1
			return true
		}); err != nil {
		return false, fmt.Errorf("is empty %s: %w", lid, err)
	} else if rows == 0 {
		return false, fmt.Errorf("%w weak coin is not set for %s", sql.ErrNotFound, lid)
	}
	return weakcoin, err
}

// SetHareOutput for the layer to a block id.
func SetHareOutput(db sql.Executor, lid types.LayerID, output types.BlockID) error {
	if _, err := db.Exec(`insert into layers (id, hare_output) values (?1, ?2) 
					on conflict(id) do update set hare_output=?2 where hare_output is null;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBytes(2, output[:])
		}, nil); err != nil {
		return fmt.Errorf("set hare output %s: %w", lid, err)
	}
	return nil
}

// prune the repetitive LayerID and BlockID.
func prune(cert *types.Certificate) *types.Certificate {
	var pruned types.Certificate
	pruned.BlockID = cert.BlockID
	pruned.Signatures = make([]types.CertifyMessage, len(cert.Signatures))
	for i, msg := range cert.Signatures {
		pruned.Signatures[i] = types.CertifyMessage{
			CertifyContent: types.CertifyContent{
				EligibilityCnt: msg.EligibilityCnt,
				Proof:          msg.Proof,
			},
			Signature: msg.Signature,
		}
	}
	return &pruned
}

// SetHareOutputWithCert sets the hare output for the layer with a block certificate.
func SetHareOutputWithCert(db sql.Executor, lid types.LayerID, cert *types.Certificate) error {
	output := cert.BlockID
	cert = prune(cert)
	data, err := codec.Encode(cert)
	if err != nil {
		return fmt.Errorf("encode cert %w", err)
	}
	if _, err := db.Exec(`insert into layers (id, hare_output, cert) values (?1, ?2, ?3) 
					on conflict(id) do update set hare_output=?2, cert=?3;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBytes(2, output[:])
			stmt.BindBytes(3, data[:])
		}, nil); err != nil {
		return fmt.Errorf("set hare output %s: %w", lid, err)
	}
	return nil
}

// GetCert returns the certificate of the hare out for the specified layer.
func GetCert(db sql.Executor, lid types.LayerID) (*types.Certificate, error) {
	var (
		cert types.Certificate
		err  error
		rows int
	)
	if rows, err = db.Exec("select cert from layers where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Value))
	}, func(stmt *sql.Statement) bool {
		data := make([]byte, stmt.ColumnLen(0))
		stmt.ColumnBytes(0, data[:])
		if err = codec.Decode(data, &cert); err == nil {
			for i := range cert.Signatures {
				cert.Signatures[i].LayerID = lid
				cert.Signatures[i].BlockID = cert.BlockID
			}
		}
		return true
	}); err != nil {
		return nil, fmt.Errorf("get cert %s: %w", lid, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w get cert %s", sql.ErrNotFound, lid)
	}
	return &cert, nil
}

// GetHareOutput for layer.
func GetHareOutput(db sql.Executor, lid types.LayerID) (rst types.BlockID, err error) {
	if rows, err := db.Exec("select hare_output from layers where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
		},
		func(stmt *sql.Statement) bool {
			if stmt.ColumnLen(0) == 0 {
				err = fmt.Errorf("%w hare output for %s is null", sql.ErrNotFound, lid)
				return false
			}
			stmt.ColumnBytes(0, rst[:])
			return true
		}); err != nil {
		return rst, fmt.Errorf("is empty %s: %w", lid, err)
	} else if rows == 0 {
		return rst, fmt.Errorf("%w hare output is not set for %s", sql.ErrNotFound, lid)
	}
	return rst, err
}

// SetApplied for the layer to a block id.
func SetApplied(db sql.Executor, lid types.LayerID, applied types.BlockID) error {
	if _, err := db.Exec(`insert into layers (id, applied_block) values (?1, ?2) 
					on conflict(id) do update set applied_block=?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBytes(2, applied[:])
		}, nil); err != nil {
		return fmt.Errorf("set applied %s: %w", lid, err)
	}
	return nil
}

// UnsetAppliedFrom updates the applied block to nil for layer >= `lid`.
func UnsetAppliedFrom(db sql.Executor, lid types.LayerID) error {
	if _, err := db.Exec("update layers set applied_block = null, state_hash = null, hash = null, aggregated_hash = null where id >= ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
		}, nil); err != nil {
		return fmt.Errorf("unset applied %s: %w", lid, err)
	}
	return nil
}

// UpdateStateHash for the layer.
func UpdateStateHash(db sql.Executor, lid types.LayerID, hash types.Hash32) error {
	if _, err := db.Exec(`insert into layers (id, state_hash) values (?1, ?2) 
	on conflict(id) do update set state_hash=?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBytes(2, hash[:])
		}, nil); err != nil {
		return fmt.Errorf("set applied %s: %w", lid, err)
	}
	return nil
}

// GetLatestStateHash loads latest state hash.
func GetLatestStateHash(db sql.Executor) (rst types.Hash32, err error) {
	if rows, err := db.Exec("select state_hash from layers where state_hash is not null;",
		nil,
		func(stmt *sql.Statement) bool {
			stmt.ColumnBytes(0, rst[:])
			return false
		}); err != nil {
		return rst, fmt.Errorf("failed to load latest state root %w", err)
	} else if rows == 0 {
		return rst, fmt.Errorf("%w: state root doesnt exist", sql.ErrNotFound)
	}
	return rst, err
}

// GetStateHash loads state hash for the layer.
func GetStateHash(db sql.Executor, lid types.LayerID) (rst types.Hash32, err error) {
	if rows, err := db.Exec("select state_hash from layers where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
		},
		func(stmt *sql.Statement) bool {
			if stmt.ColumnLen(0) == 0 {
				err = fmt.Errorf("%w: state_hash for %s is not set", sql.ErrNotFound, lid)
				return false
			}
			stmt.ColumnBytes(0, rst[:])
			return false
		}); err != nil {
		return rst, fmt.Errorf("failed to load state root for %v: %w", lid, err)
	} else if rows == 0 {
		return rst, fmt.Errorf("%w: %s doesnt exist", sql.ErrNotFound, lid)
	}
	return rst, err
}

// GetApplied for the applied block for layer.
func GetApplied(db sql.Executor, lid types.LayerID) (rst types.BlockID, err error) {
	if rows, err := db.Exec("select applied_block from layers where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
		},
		func(stmt *sql.Statement) bool {
			if stmt.ColumnLen(0) == 0 {
				err = fmt.Errorf("%w applied for %s is null", sql.ErrNotFound, lid)
				return false
			}
			stmt.ColumnBytes(0, rst[:])
			return true
		}); err != nil {
		return rst, fmt.Errorf("is empty %s: %w", lid, err)
	} else if rows == 0 {
		return rst, fmt.Errorf("%w applied is not set for %s", sql.ErrNotFound, lid)
	}
	return rst, err
}

// GetLastApplied for the applied block for layer.
func GetLastApplied(db sql.Executor) (types.LayerID, error) {
	var lid types.LayerID
	if _, err := db.Exec("select max(id) from layers where applied_block is not null", nil,
		func(stmt *sql.Statement) bool {
			lid = types.NewLayerID(uint32(stmt.ColumnInt64(0)))
			return true
		}); err != nil {
		return lid, fmt.Errorf("last applied: %w", err)
	}
	return lid, nil
}

// SetProcessed sets a layer processed.
func SetProcessed(db sql.Executor, lid types.LayerID) error {
	if _, err := db.Exec(
		`insert into layers (id, processed) values (?1, 1) 
         on conflict(id) do update set processed=1;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Uint32()))
		}, nil); err != nil {
		return fmt.Errorf("set processed %v: %w", lid, err)
	}
	return nil
}

// GetProcessed gets the highest layer processed.
func GetProcessed(db sql.Executor) (types.LayerID, error) {
	var lid types.LayerID
	if _, err := db.Exec("select max(id) from layers where processed = 1;",
		nil,
		func(stmt *sql.Statement) bool {
			lid = types.NewLayerID(uint32(stmt.ColumnInt64(0)))
			return true
		}); err != nil {
		return lid, fmt.Errorf("processed layer: %w", err)
	}
	return lid, nil
}

// SetHashes sets the layer hash and aggregated hash.
func SetHashes(db sql.Executor, lid types.LayerID, hash, aggHash types.Hash32) error {
	if _, err := db.Exec(
		`insert into layers (id, hash, aggregated_hash) values (?1, ?2, ?3) 
         on conflict(id) do update set hash=?2, aggregated_hash=?3;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Uint32()))
			stmt.BindBytes(2, hash[:])
			stmt.BindBytes(3, aggHash[:])
		}, nil); err != nil {
		return fmt.Errorf("set hashes %v: %w", lid, err)
	}
	return nil
}

func getHash(db sql.Executor, field string, lid types.LayerID) (rst types.Hash32, err error) {
	if rows, err := db.Exec(fmt.Sprintf("select %s from layers where id = ?1", field),
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Uint32()))
		},
		func(stmt *sql.Statement) bool {
			stmt.ColumnBytes(0, rst[:])
			return true
		}); err != nil {
		return rst, fmt.Errorf("select %s for %s: %w", field, lid, err)
	} else if rows == 0 {
		return rst, fmt.Errorf("%w layer %s", sql.ErrNotFound, lid)
	}
	return rst, err
}

// GetHash for layer.
func GetHash(db sql.Executor, lid types.LayerID) (types.Hash32, error) {
	return getHash(db, hashField, lid)
}

// GetAggregatedHash for layer.
func GetAggregatedHash(db sql.Executor, lid types.LayerID) (types.Hash32, error) {
	return getHash(db, aggregatedHashField, lid)
}
