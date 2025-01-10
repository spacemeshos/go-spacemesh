package vm

import (
	"bytes"
	"errors"
	"fmt"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/spacemeshos/go-scale"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/vm/core"
	vmhost "github.com/spacemeshos/go-spacemesh/vm/host"
)

func verify(ctx *core.Context, logger *zap.Logger) error {
	hash := core.HashTx(ctx.TxData)
	// TODO(lane): re-add support for genesisID
	// see https://github.com/athenavm/athena/issues/178
	// signedData := core.SigningBody(host.GetGenesisID().Bytes(), raw[:len(raw)-n])
	signedData := hash[:]

	// The input to the verify method must be SCALE-encoded.
	var verifyArgsEncoded bytes.Buffer
	encoder := scale.NewEncoder(&verifyArgsEncoded)
	_, err := scale.EncodeByteSlice(encoder, signedData)
	if err != nil {
		return fmt.Errorf("marshalling verify args: %w", err)
	}
	verifyArgsEncoded.Write(ctx.WitnessData)

	// FIXME: it makes no sense to have signed max gas...
	// It needs to be changed in the athena VM library to use unsigned integer.
	maxgas := int64(ctx.MaxGas())
	if maxgas < 0 {
		return fmt.Errorf("negative maxgas: %d", maxgas)
	}

	// Instantiate the VM
	// Use a mock host to ensure that no state changes occur.
	host := ctx.Clone()
	vmhost, err := vmhost.NewHost(host, logger)
	if err != nil {
		return fmt.Errorf("creating new host: %w", err)
	}
	defer vmhost.Destroy()

	accountState := ctx.PrincipalAccount.State
	// If this is a spawn transaction, the wallet state is currently empty. So we need to
	// provisionally spawn the wallet program instance so we can call the verify method.
	if ctx.IsSpawn() {
		if len(accountState) != 0 {
			// TODO(lane): should we allow spawn to be called multiple times on the same account?
			return errors.New("cannot spawn multiple times")
		}

		// the transaction must already be a spawn tx, so there's no need to modify the payload.
		executionPayload := athcon.EncodedExecutionPayload(nil, ctx.TxPayload)
		logger.Debug(
			"provisionally executing spawn",
			zap.Uint32("layer", ctx.Layer().Uint32()),
			zap.Int64("maxgas", maxgas),
		)
		_, gasLeft, err := vmhost.Execute(
			ctx.Layer(),
			maxgas,
			ctx.Principal(),
			ctx.Principal(),
			executionPayload,
			ctx.TemplateCode,
		)
		if err != nil {
			return fmt.Errorf("executing auto-spawn: %w", err)
		}
		logger.Debug("auto-spawn finished", zap.Int64("gas", maxgas-gasLeft))

		// the account should've been spawned
		spawnedAccount, err := host.Get(ctx.Principal())
		if err != nil {
			return fmt.Errorf("spawn failed - account not found: %w", err)
		}
		accountState = spawnedAccount.State
	}

	// construct the payload: wallet state + payload (method selector + input (raw tx + signature))
	verifySelector, _ := athcon.FromString("athexp_verify")
	payload := athcon.Payload{
		Selector: &verifySelector,
		Input:    verifyArgsEncoded.Bytes(),
	}
	payloadEncoded, err := gossamerScale.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling verify payload: %w", err)
	}
	executionPayload := athcon.EncodedExecutionPayload(accountState, payloadEncoded)

	logger.Debug("executing verify", zap.Uint32("layer", ctx.Layer().Uint32()), zap.Int64("maxgas", maxgas))
	output, gasLeft, err := vmhost.Execute(
		ctx.Layer(),
		maxgas,
		ctx.Principal(),
		ctx.Principal(),
		executionPayload,
		ctx.TemplateCode,
	)

	// consume verify gas
	// TODO(lane): safe arithmetic/assumption checking
	ctx.SpendGas(uint64(maxgas - gasLeft))
	if err != nil {
		return fmt.Errorf("verifying TX: %w", err)
	}
	if len(output) == 0 {
		return errors.New("empty verify output")
	}
	logger.Debug("verify finished",
		zap.Int64("actual gas", maxgas-gasLeft),
		zap.Bool("valid", output[0] == 1),
	)
	if output[0] != 1 {
		return errors.New("TX didn't pass verification")
	}
	return nil
}
