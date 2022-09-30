package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenesisID(t *testing.T) {
	t.Run("changes based on cfg vars", func(t *testing.T) {
		cfg1 := GenesisConfig{ExtraData: "one", GenesisTime: "two"}
		cfg2 := GenesisConfig{ExtraData: "one", GenesisTime: "one"}
		require.NotEqual(t, cfg1.GenesisID(), cfg2.GenesisID())
	})
	t.Run("consistent", func(t *testing.T) {
		cfg := GenesisConfig{ExtraData: "one", GenesisTime: "two"}
		require.Equal(t, cfg.GenesisID(), cfg.GenesisID())
	})
	t.Run("stable", func(t *testing.T) {
		cfg := GenesisConfig{ExtraData: "one", GenesisTime: "two"}
		require.Equal(t, "0x91d338938929ec38e320ba558b6bd8538eae972d", cfg.GenesisID().Hex())
	})
}
