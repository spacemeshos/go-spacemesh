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

func AddCertificate(db sql.Executor, nodeID types.NodeID, cert PoetCert, poetUrl string) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindBytes(2, []byte(poetUrl))
		stmt.BindBytes(3, cert.Data)
		stmt.BindBytes(4, cert.Signature)
	}
	if _, err := db.Exec(`
		REPLACE INTO poet_certificates (node_id, poet_url, certificate, signature)
		VALUES (?1, ?2, ?3, ?4);`, enc, nil,
	); err != nil {
		return fmt.Errorf("storing poet certificate for (%s; %s): %w", nodeID.ShortString(), poetUrl, err)
	}
	return nil
}

func Certificate(db sql.Executor, nodeID types.NodeID, poetUrl string) (*PoetCert, error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindBytes(2, []byte(poetUrl))
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
		from poet_certificates where node_id = ?1 and poet_url = ?2 limit 1;`, enc, dec,
	)
	switch {
	case err != nil:
		return nil, fmt.Errorf("getting poet certificate for (%s; %s): %w", nodeID.ShortString(), poetUrl, err)
	case rows == 0:
		return nil, sql.ErrNotFound
	}
	return &cert, nil
}
