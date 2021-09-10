package tortoisebeacon

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func newMemDB(tb testing.TB) *DB {
	tb.Helper()
	return NewDB(database.NewMemDatabase(), logtest.New(tb))
}
