package sql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVacuumDB(t *testing.T) {
	db := InMemory(withIgnoreSchemaDrift())
	require.NoError(t, Vacuum(db))
}
