package cmd

import (
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/cmd/client/api"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
)

var path string

// deployCmd represents the deploy command.
var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "A brief description of your command",
	RunE: func(cmd *cobra.Command, args []string) error {
		if nonce == math.MaxUint64 {
			return errors.New("provide nonce counter with --nonce")
		}
		key, err := getKey(0)
		if err != nil {
			return err
		}
		return deploy(key, nonce, path)
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
	deployCmd.Flags().StringVar(&path, "path", "", "path to contract template to deploy")
}

func deploy(principal signing.PrivateKey, nonce uint64, path string) error {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	principialPubkey := signing.Public(principal)
	principialAddress := wallet.Address(principialPubkey)

	blob, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading contract from %s: %w", path, err)
	}
	tx, err := wallet.Deploy(principal, nonce, blob)
	if err != nil {
		return err
	}

	logger.Info("deploying contract",
		zap.Stringer("ID", types.NewRawTx(tx).ID),
		zap.Stringer("principal", principialAddress),
		zap.Stringer("template address", core.TemplateAddress(blob)),
	)

	if draft {
		logger.Info("not submitting TX in draft mode")
		return nil
	}
	return api.Submit(address, tx, logger)
}
