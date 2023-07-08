package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/hash"
)

func TestGenesisID(t *testing.T) {
	t.Run("changes based on cfg vars", func(t *testing.T) {
		cfg1 := GenesisConfig{ExtraData: "one", GenesisTime: "2023-03-15T18:00:00Z"}
		cfg2 := GenesisConfig{ExtraData: "one", GenesisTime: "1989-03-15T18:00:00Z"}
		require.NoError(t, cfg1.Validate())
		require.NotEqual(t, cfg1.GenesisID(), cfg2.GenesisID())
	})
	t.Run("require non-empty", func(t *testing.T) {
		cfg := GenesisConfig{GenesisTime: "2023-03-15T18:00:00Z"}
		require.ErrorContains(t, cfg.Validate(), "wait until extra-data is available")
	})
	t.Run("consistent", func(t *testing.T) {
		cfg := GenesisConfig{ExtraData: "one", GenesisTime: "2023-03-15T18:00:00Z"}
		require.NoError(t, cfg.Validate())
		require.Equal(t, cfg.GenesisID(), cfg.GenesisID())
	})
	t.Run("stable", func(t *testing.T) {
		unixtime := time.Unix(10101, 0)
		cfg := GenesisConfig{ExtraData: "one", GenesisTime: unixtime.Format(time.RFC3339)}
		require.NoError(t, cfg.Validate())

		expected := hash.Sum([]byte("10101"), []byte("one"))
		require.Equal(t, expected[:20], cfg.GenesisID().Bytes())
	})
}
