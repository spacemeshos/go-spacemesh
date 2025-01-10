package cmd

import (
	"errors"
	"math"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/cmd/client/api"
	"github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
)

// spawnCmd represents the spawn command.
var spawnCmd = &cobra.Command{
	Use:   "spawn",
	Short: "spawn a wallet account",
	RunE: func(cmd *cobra.Command, args []string) error {
		if nonce == math.MaxUint64 {
			return errors.New("provide nonce counter with --nonce")
		}
		key, err := getKey(0)
		if err != nil {
			return err
		}
		return spawn(address, key, nonce)
	},
}

func init() {
	rootCmd.AddCommand(spawnCmd)
}

func spawn(address string, privKey signing.PrivateKey, nonce uint64) error {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	principialPubkey := signing.Public(privKey)
	principalAddress := wallet.Address(principialPubkey)
	tx, err := wallet.Spawn(privKey, nonce)
	if err != nil {
		return err
	}
	logger.Info(
		"spawning account",
		zap.Stringer("address", principalAddress),
		zap.Stringer("id", types.NewRawTx(tx).ID),
	)

	if draft {
		logger.Info("not submitting TX in draft mode")
		return nil
	}
	return api.Submit(address, tx, logger)
}
