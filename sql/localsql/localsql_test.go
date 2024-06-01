package localsql

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/sql/test"
)

var fns = test.DBFuncs[*Database]{Schema: Schema, Open: Open, InMemory: InMemory}

func TestDatabase_MigrateTwice_NoOp(t *testing.T) {
	test.VerifyMigrateTwiceNoOp(t, fns)
}

func TestSchema(t *testing.T) {
	test.VerifySchema(t, fns)
}
