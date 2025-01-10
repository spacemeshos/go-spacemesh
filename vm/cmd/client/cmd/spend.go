package cmd

import (
	"errors"
	"fmt"
	"math"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/cmd/client/api"
	"github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
)

var (
	to     string
	amount uint64
)

// spendCmd represents the spend command.
var spendCmd = &cobra.Command{
	Use:   "spend",
	Short: "send tokens to other account",
	RunE: func(cmd *cobra.Command, args []string) error {
		if nonce == math.MaxUint64 {
			return errors.New("provide nonce counter with --nonce")
		}
		if amount == math.MaxUint64 {
			return errors.New("provide nonce counter with --amount")
		}
		key, err := getKey(0)
		if err != nil {
			return err
		}
		recipientAddress, err := types.StringToAddress(to)
		if err != nil {
			return fmt.Errorf("cannot parse recipient address %q: %w", to, err)
		}
		return spend(address, key, recipientAddress, amount, nonce)
	},
}

func init() {
	rootCmd.AddCommand(spendCmd)
	spendCmd.Flags().Uint64Var(&amount, "amount", math.MaxUint64, "Nonce for the spawn transaction")
	spendCmd.Flags().StringVar(&to, "to", "", "transfer recipient address")
}

func spend(address string, senderKey signing.PrivateKey, recipientAddress types.Address, amount, nonce uint64) error {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	principialAddress := wallet.Address(signing.Public(senderKey))

	logger.Info(
		"sending coins",
		zap.Uint64("amount", amount),
		zap.Uint64("nonce", nonce),
		zap.Stringer("from", principialAddress),
		zap.Stringer("to", recipientAddress),
	)
	tx, err := wallet.Spend(senderKey, recipientAddress, amount, nonce)
	if err != nil {
		return err
	}

	if draft {
		logger.Info("not submitting TX in draft mode")
		return nil
	}
	return api.Submit(address, tx, logger)
}
