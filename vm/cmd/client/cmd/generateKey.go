package cmd

import (
	"log"

	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spf13/cobra"
)

var file string

var generateKeyCmd = &cobra.Command{
	Use:   "generateKey",
	Short: "Generate a keypair for signing transacations",
	Run: func(cmd *cobra.Command, args []string) {
		_, err := signing.NewEdSigner(signing.ToFile(file))
		if err != nil {
			log.Fatalf("failed to create keys: %s", err.Error())
		}
	},
}

func init() {
	rootCmd.AddCommand(generateKeyCmd)

	generateKeyCmd.Flags().StringVar(&file, "file", "key.hex", "file to save the keys in")
}
