package cmd

import (
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFlag_SmeshingOptsVerifyingDisable(t *testing.T) {
	var c cobra.Command
	AddCommands(&c)
	t.Run("default is false", func(t *testing.T) {
		t.Cleanup(ResetConfig)
		require.NoError(t, c.ParseFlags([]string{}))
		require.False(t, cfg.SMESHING.VerifyingOpts.Disabled)
	})
	t.Run("set flag", func(t *testing.T) {
		t.Cleanup(ResetConfig)
		require.NoError(t, c.ParseFlags([]string{"--smeshing-opts-verifying-disable"}))
		require.True(t, cfg.SMESHING.VerifyingOpts.Disabled)
	})
}
