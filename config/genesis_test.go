package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/hash"
)

func TestGenesisJsonEncode(t *testing.T) {
	tString := "2023-03-15T18:00:00Z"
	t1, err := time.Parse(time.RFC3339, tString)
	require.NoError(t, err)

	t.Run("marshal", func(t *testing.T) {
		g := Genesis(t1)
		b, err := g.MarshalJSON()
		require.NoError(t, err)
		expected, err := json.Marshal(tString)
		require.NoError(t, err)
		require.Equal(t, expected, b)
	})
	t.Run("unmarshal", func(t *testing.T) {
		reference, err := json.Marshal(tString)
		require.NoError(t, err)
		var g Genesis
		err = g.UnmarshalJSON([]byte(reference))
		require.NoError(t, err)
		require.Equal(t, t1, g.Time())
	})
}

func TestGenesisTextUnmarshal(t *testing.T) {
	tString := "2023-03-15T18:00:00Z"
	t1, err := time.Parse(time.RFC3339, tString)
	require.NoError(t, err)

	var g Genesis
	err = g.UnmarshalText([]byte(tString))
	require.NoError(t, err)
	require.Equal(t, t1, g.Time())
}

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
