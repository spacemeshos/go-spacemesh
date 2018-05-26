package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	cfg "github.com/spacemeshos/go-spacemesh/config"
)

var (
	config = cfg.DefaultConfig()
)

var RootCmd = &cobra.Command{
	Use: "spacemesh",
	Short: "Spacemesh Core ( PoST )",
	PersistentPreRun: func(cmd *cobra.Command, args[] string){
		// read in default config if passed as param using viper

	},
}

RootCmd.PersistentFlags().StringVar(&config.)