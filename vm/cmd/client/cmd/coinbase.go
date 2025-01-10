package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
)

var coinbaseCmd = &cobra.Command{
	Use:   "coinbase",
	Short: "print coinbase for the chosen key (assuming the singlesig wallet template)",
	RunE: func(cmd *cobra.Command, args []string) error {
		key, err := getKey(0)
		if err != nil {
			return err
		}
		address := wallet.Address(signing.Public(key))

		fmt.Printf("coinbase: %s\n", address.String())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(coinbaseCmd)
}
