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
		require.Equal(t, "0x374ea0818fe293007e99bc08185cac7a12aea8dd", cfg.GenesisID().Hex())
	})
}
