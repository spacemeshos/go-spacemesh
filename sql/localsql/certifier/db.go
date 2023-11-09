package certifier

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func AddCertificate(db sql.Executor, nodeID types.NodeID, cert []byte, poetUrl string) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindBytes(2, []byte(poetUrl))
		stmt.BindBytes(3, cert)
	}
	if _, err := db.Exec(`
		insert into poet_certificates (node_id, poet_url, certificate)
		values (?1, ?2, ?3);`, enc, nil,
	); err != nil {
		return fmt.Errorf("storing poet certificate for (%s; %s): %w", nodeID.ShortString(), poetUrl, err)
	}
	return nil
}

func Certificate(db sql.Executor, nodeID types.NodeID, poetUrl string) ([]byte, error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindBytes(2, []byte(poetUrl))
	}
	var cert []byte
	dec := func(stmt *sql.Statement) bool {
		cert = make([]byte, stmt.ColumnLen(0))
		stmt.ColumnBytes(0, cert)
		return true
	}
	rows, err := db.Exec(`
		select certificate
		from poet_certificates where node_id = ?1 and poet_url = ?2 limit 1;`, enc, dec,
	)
	switch {
	case err != nil:
		return nil, fmt.Errorf("getting poet certificate for (%s; %s): %w", nodeID.ShortString(), poetUrl, err)
	case rows == 0:
		return nil, sql.ErrNotFound
	}
	return cert, nil
}
