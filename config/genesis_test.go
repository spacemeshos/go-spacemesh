package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/hash"
)

func TestGenesisID(t *testing.T) {
	t1, err := time.Parse(time.RFC3339, "2023-03-15T18:00:00Z")
	require.NoError(t, err)
	t2, err := time.Parse(time.RFC3339, "1989-03-15T18:00:00Z")
	require.NoError(t, err)

	t.Run("changes based on cfg vars", func(t *testing.T) {
		cfg1 := GenesisConfig{ExtraData: "one", GenesisTime: Genesis(t1)}
		cfg2 := GenesisConfig{ExtraData: "one", GenesisTime: Genesis(t2)}
		require.NoError(t, cfg1.Validate())
		require.NotEqual(t, cfg1.GenesisID(), cfg2.GenesisID())
	})
	t.Run("require non-empty", func(t *testing.T) {
		cfg := GenesisConfig{GenesisTime: Genesis(t1)}
		require.ErrorContains(t, cfg.Validate(), "wait until genesis-extra-data is available")
	})
	t.Run("consistent", func(t *testing.T) {
		cfg := GenesisConfig{ExtraData: "one", GenesisTime: Genesis(t1)}
		require.NoError(t, cfg.Validate())
		genID := cfg.GenesisID()
		require.Equal(t, genID, cfg.GenesisID())
	})
	t.Run("stable", func(t *testing.T) {
		unixTime := time.Unix(10101, 0)
		cfg := GenesisConfig{ExtraData: "one", GenesisTime: Genesis(unixTime)}
		require.NoError(t, cfg.Validate())

		expected := hash.Sum([]byte("10101"), []byte("one"))
		require.Equal(t, expected[:20], cfg.GenesisID().Bytes())
	})
}
