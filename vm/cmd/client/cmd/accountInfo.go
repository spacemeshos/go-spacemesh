package cmd

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/cmd/client/api"
	"github.com/spf13/cobra"
)

var accountAddress string

var accountInfoCmd = &cobra.Command{
	Use:   "accountInfo",
	Short: "A brief description of your command",
	RunE: func(cmd *cobra.Command, args []string) error {
		if accountAddress == "" {
			return errors.New("please provide account address to check with --account")
		}
		accountAddress, err := types.StringToAddress(accountAddress)
		if err != nil {
			return fmt.Errorf("cannot parse recipient address %q: %w", to, err)
		}
		account, err := api.AccountInfo(address, accountAddress)
		if err != nil {
			return err
		}
		fmt.Printf("template       : %s\n", account.Template)
		fmt.Printf("current state  : %s\n", account.Current.String())
		fmt.Printf("projected state: %s\n", account.Projected.String())

		return nil
	},
}

func init() {
	rootCmd.AddCommand(accountInfoCmd)
	accountInfoCmd.Flags().StringVar(&accountAddress, "account", "", "account address to check")

}
