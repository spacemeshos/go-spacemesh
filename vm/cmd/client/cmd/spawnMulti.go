package cmd

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"

	"github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/cmd/client/api"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/templates/multisig"
)

var (
	requiredFlag uint8
	keyIdxsFlag  []int
)

// spawnMultiCmd represents the spawnMulti command.
var spawnMultiCmd = &cobra.Command{
	Use:   "spawnMulti",
	Short: "A brief description of your command",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(keyIdxsFlag) == 0 {
			return errors.New("please provide key indicies")
		}
		if requiredFlag == 0 {
			return errors.New("provide the number of required signatures")
		}
		if int(requiredFlag) > len(keyIdxsFlag) {
			return errors.New("cannot require more signatures than keys")
		}
		pks := make([]signing.PrivateKey, 0, len(keyIdxsFlag))
		for _, i := range keyIdxsFlag {
			key, err := getKey(i)
			if err != nil {
				return err
			}
			pks = append(pks, key)
		}
		return spawnMulti(address, nonce, requiredFlag, pks)
	},
}

func init() {
	rootCmd.AddCommand(spawnMultiCmd)
	spawnMultiCmd.Flags().
		IntSliceVar(&keyIdxsFlag, "keys", nil, "indices of keys to spawn multisig account with. supports [0-10].")
	spawnMultiCmd.Flags().Uint8Var(&requiredFlag, "required", 0, "number of required signatures on a multisig TX")
}

type multiSignature struct {
	lastID    uint8
	signature []byte
}

func (s *multiSignature) sign(tx []byte, id uint8, pk signing.PrivateKey) error {
	if id <= s.lastID {
		return fmt.Errorf("id %d must be greater than last ID %d", id, s.lastID)
	}

	s.signature = append(s.signature, id)
	s.signature = append(s.signature, ed25519.Sign(ed25519.PrivateKey(pk), tx)...)
	return nil
}

type multiSigwallet struct {
	required uint8
	keys     []signing.PrivateKey
}

func (w *multiSigwallet) sign(tx []byte, keys []uint8) ([]byte, error) {
	var signature multiSignature

	slices.Sort(keys)
	for _, id := range keys {
		if int(id) > len(w.keys) {
			return nil, fmt.Errorf("key ID %d out of range %d", id, len(w.keys))
		}
		signature.sign(tx, id, w.keys[id])
	}
	return signature.signature, nil
}

func (w *multiSigwallet) Spawn(template types.Address) (*types.Address, []byte, error) {
	type spawnArguments struct {
		Required uint8
		Pubkeys  [][32]byte
	}
	args := spawnArguments{
		Required: w.required,
		Pubkeys:  make([][32]byte, 0, len(w.keys)),
	}
	for _, k := range w.keys {
		var pub [32]byte
		copy(pub[:], signing.Public(k))
		args.Pubkeys = append(args.Pubkeys, pub)
	}

	encodedArgs, err := scale.Marshal(args)
	if err != nil {
		return nil, nil, fmt.Errorf("marshalling spawn args: %w", err)
	}
	selector, _ := athcon.FromString("athexp_spawn")
	payload := athcon.Payload{
		Selector: &selector,
		Input:    encodedArgs,
	}
	encodedPayload, err := scale.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding TX payload: %w", err)
	}
	accountAddress := core.ComputePrincipalFromBlob(template, encodedArgs)
	tx := core.Tx{
		Version:   1,
		Principal: accountAddress,
		Template:  &template,
		Metadata: core.Metadata{
			Nonce:    0,
			GasPrice: 1,
		},
		Payload: encodedPayload,
	}
	rawTx, err := codec.Encode(&tx)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding TX: %w", err)
	}

	keys := make([]uint8, len(w.keys))
	for id := range w.keys {
		keys[id] = uint8(id)
	}
	sig, err := w.sign(rawTx, keys)
	if err != nil {
		return nil, nil, fmt.Errorf("signing: %w", err)
	}
	return &accountAddress, append(rawTx, sig...), nil
}

func spawnMulti(address string, nonce uint64, required uint8, keys []signing.PrivateKey) error {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}

	wallet := multiSigwallet{required: required, keys: keys}
	templateAddress := multisig.TemplateAddress
	accountAddress, tx, err := wallet.Spawn(templateAddress)
	if err != nil {
		return fmt.Errorf("creating spawn TX: %w", err)
	}

	logger.Info(
		"spawning multi-sig account",
		zap.Stringer("address", accountAddress),
		zap.Stringer("template address", templateAddress),
	)

	if draft {
		logger.Info("not submitting TX in draft mode")
		return nil
	}
	return api.Submit(address, tx, logger)
}
