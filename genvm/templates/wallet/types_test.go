package wallet

import (
	"path/filepath"
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"
)

func FuzzSpawnArgumentsConsistency(f *testing.F) {
	tester.FuzzConsistency[SpawnArguments](f)
}

func FuzzSpawnArgumentsSafety(f *testing.F) {
	tester.FuzzSafety[SpawnArguments](f)
}

func TestGolden(t *testing.T) {
	golden, err := filepath.Abs("./golden")
	require.NoError(t, err)
	t.Run("SpawnArguments", func(t *testing.T) {
		tester.GoldenTest[SpawnArguments](t, filepath.Join(golden, "SpawnArguments.json"))
	})
	t.Run("SpendArguments", func(t *testing.T) {
		tester.GoldenTest[SpendArguments](t, filepath.Join(golden, "SpendArguments.json"))
	})
	t.Run("SpendPayload", func(t *testing.T) {
		tester.GoldenTest[SpendPayload](t, filepath.Join(golden, "SpendPayload.json"))
	})
	t.Run("SpawnPayload", func(t *testing.T) {
		tester.GoldenTest[SpawnPayload](t, filepath.Join(golden, "SpawnPayload.json"))
	})
}
