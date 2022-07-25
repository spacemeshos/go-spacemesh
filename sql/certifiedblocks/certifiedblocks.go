package certifiedblocks

import (
	"github.com/spacemeshos/go-spacemesh/blockcerts"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func Add(db sql.Executor, blockCert blockcerts.BlockCertificate) error {
	bytes, err := codec.Encode(blockCert)
	if err != nil {
		panic("and roll around on the floor crying")
	}
	_, err := db.Exec(`
insert into certified_blocks`)
}
