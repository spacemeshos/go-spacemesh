package sql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVacuumDB(t *testing.T) {
	db := InMemory()
	require.NoError(t, Vacuum(db))
}
