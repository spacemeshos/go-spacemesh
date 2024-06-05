package statesql

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/test"
)

type fns struct{}

func (fns) Schema() (*sql.Schema, error)                        { return Schema() }
func (fns) InMemory(opts ...sql.Opt) *Database                  { return InMemory(opts...) }
func (fns) Open(uri string, opts ...sql.Opt) (*Database, error) { return Open(uri, opts...) }

func TestSchema(t *testing.T) {
	test.RunSchemaTests(t, fns{})
}
