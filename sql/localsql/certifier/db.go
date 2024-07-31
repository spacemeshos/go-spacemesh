package certifier

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type PoetCert struct {
	Data      []byte
	Signature []byte
}

func AddCertificate(db sql.Executor, nodeID types.NodeID, cert PoetCert, cerifierID []byte) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindBytes(2, cerifierID)
		stmt.BindBytes(3, cert.Data)
		stmt.BindBytes(4, cert.Signature)
	}
	if _, err := db.Exec(`
		REPLACE INTO poet_certificates (node_id, certifier_id, certificate, signature)
		VALUES (?1, ?2, ?3, ?4);`, enc, nil,
	); err != nil {
		return fmt.Errorf("storing poet certificate for (%s; %x): %w", nodeID.ShortString(), cerifierID, err)
	}
	return nil
}

func DeleteCertificate(db sql.Executor, nodeID types.NodeID, certifierID []byte) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindBytes(2, certifierID)
	}
	if _, err := db.Exec(`
		DELETE FROM poet_certificates WHERE node_id = ?1 AND certifier_id = ?2;`, enc, nil,
	); err != nil {
		return fmt.Errorf("deleting poet certificate for (%s; %x): %w", nodeID.ShortString(), certifierID, err)
	}
	return nil
}

func Certificate(db sql.Executor, nodeID types.NodeID, certifierID []byte) (*PoetCert, error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindBytes(2, certifierID)
	}
	var cert PoetCert
	dec := func(stmt *sql.Statement) bool {
		cert.Data = make([]byte, stmt.ColumnLen(0))
		cert.Signature = make([]byte, stmt.ColumnLen(1))
		stmt.ColumnBytes(0, cert.Data)
		stmt.ColumnBytes(1, cert.Signature)
		return true
	}
	rows, err := db.Exec(`
		select certificate, signature
		from poet_certificates where node_id = ?1 and certifier_id = ?2 limit 1;`, enc, dec,
	)
	switch {
	case err != nil:
		return nil, fmt.Errorf("getting poet certificate for (%s; %s): %w", nodeID.ShortString(), certifierID, err)
	case rows == 0:
		return nil, sql.ErrNotFound
	}
	return &cert, nil
}
