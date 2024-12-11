package wallet

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/spacemeshos/go-scale"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/vm/core"
	vmhost "github.com/spacemeshos/go-spacemesh/vm/host"
)

func init() {
	TemplateAddress[len(TemplateAddress)-1] = 1
}

var TemplateAddress core.Address

// New returns Wallet instance with SpawnArguments.
func New(host core.Host, logger *zap.Logger) (*Wallet, error) {
	// Load the template account
	templateAccount, err := host.Get(host.TemplateAddress())
	if err != nil {
		return nil, fmt.Errorf("new wallet template: failed to load template account: %w", err)
	} else if len(templateAccount.State) == 0 {
		return nil, errors.New("new wallet template: template account state is empty")
	}
	templateCode := templateAccount.State

	// Load the wallet state
	walletAccount, err := host.Get(host.Principal())
	if err != nil {
		return nil, fmt.Errorf("new wallet template: failed to load wallet principal account: %w", err)
	} else if len(walletAccount.State) == 0 && !host.IsSpawn() {
		// If this is a spawn we expect the current state to be empty
		return nil, errors.New("new wallet template: wallet account state is empty for non-spawn")
	} else if len(walletAccount.State) != 0 && host.IsSpawn() {
		// If this is a spawn we expect the current state to be empty
		return nil, errors.New("new wallet template: wallet account state is not empty for spawn")
	}
	walletState := walletAccount.State

	return &Wallet{host, templateCode, walletState, logger}, nil
}

// Wallet is a single-key wallet.
type Wallet struct {
	host         core.Host
	templateCode []byte
	walletState  []byte
	logger       *zap.Logger
}

// MaxSpend returns amount specified in the SpendArguments for Spend method.
func (s *Wallet) MaxSpend(payload []byte) (uint64, error) {
	maxgas := int64(s.host.MaxGas())
	if maxgas < 0 {
		return 0, errors.New("gas limit exceeds maximum int64 value")
	}

	var unmarshaled athcon.Payload
	err := gossamerScale.Unmarshal(payload, &unmarshaled)
	if err != nil {
		return 0, fmt.Errorf("%w: malformed spawn payload", core.ErrMalformed)
	}

	// Check the method selector
	// We define MaxSpend for any method other than spend to be zero for now.
	spendSelector, _ := athcon.FromString("athexp_spend")
	if unmarshaled.Selector == nil || *unmarshaled.Selector != spendSelector {
		return 0, nil
	}

	// construct the payload. this requires some surgery to replace the method maxSpendSelector.
	maxSpendSelector, _ := athcon.FromString("athexp_max_spend")
	maxGasPayload := athcon.Payload{
		Selector: &maxSpendSelector,
		Input:    unmarshaled.Input,
	}
	maxGasPayloadEncoded, err := gossamerScale.Marshal(maxGasPayload)
	if err != nil {
		return 0, fmt.Errorf("marshaling maxSpend payload: %w", err)
	}
	executionPayload := athcon.EncodedExecutionPayload(s.walletState, maxGasPayloadEncoded)

	// Instantiate the VM
	// Use a mock host to ensure that no state changes occur.
	host := s.host.Clone()
	vmhost, err := vmhost.NewHost(host, s.logger)
	if err != nil {
		return 0, fmt.Errorf("loading Athena VM: %w", err)
	}
	defer vmhost.Destroy()

	s.logger.Debug("executing maxspend", zap.Uint32("layer", s.host.Layer().Uint32()), zap.Int64("maxgas", maxgas))
	output, _, err := vmhost.Execute(
		s.host.Layer(),
		maxgas,
		s.host.Principal(),
		s.host.Principal(),
		executionPayload,
		0,
		s.templateCode,
	)
	var maxspend uint64
	if err == nil {
		if len(output) != 8 {
			return 0, errors.New("max spend output is not 8 bytes")
		}
		maxspend = binary.LittleEndian.Uint64(output)
	}
	return maxspend, err
}

// Verify the transaction signature using the VM.
func (s *Wallet) Verify(tx, witnessData []byte) error {
	hash := core.HashTx(tx)
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
	verifyArgsEncoded.Write(witnessData)

	maxgas := int64(s.host.MaxGas())
	if maxgas < 0 {
		return fmt.Errorf("negative maxgas: %d", maxgas)
	}

	// Instantiate the VM
	// Use a mock host to ensure that no state changes occur.
	host := s.host.Clone()
	vmhost, err := vmhost.NewHost(host, s.logger)
	if err != nil {
		return fmt.Errorf("creating new host: %w", err)
	}
	defer vmhost.Destroy()

	// If this is a spawn transaction, the wallet state is currently empty. So we need to
	// provisionally spawn the wallet program instance so we can call the verify method.
	if s.host.IsSpawn() {
		if len(s.walletState) != 0 {
			// TODO(lane): should we allow spawn to be called multiple times on the same account?
			return errors.New("cannot spawn multiple times")
		}

		// the transaction must already be a spawn tx, so there's no need to modify the payload.
		executionPayload := athcon.EncodedExecutionPayload(nil, s.host.Payload())
		s.logger.Debug("executing spawn", zap.Uint32("layer", s.host.Layer().Uint32()), zap.Int64("maxgas", maxgas))
		_, gasLeft, err := vmhost.Execute(
			s.host.Layer(),
			maxgas,
			s.host.Principal(),
			s.host.Principal(),
			executionPayload,
			0,
			s.templateCode,
		)
		if err != nil {
			return fmt.Errorf("executing auto-spawn: %w", err)
		}
		s.logger.Debug("auto-spawn finished", zap.Int64("gas", maxgas-gasLeft))

		// the account should've been spawned
		walletAccount, err := host.Get(s.host.Principal())
		if err != nil {
			return fmt.Errorf("spawn failed - account not found: %w", err)
		}
		if len(walletAccount.State) == 0 {
			s.logger.Panic("wallet acount is empty after spawn - this should never happen")
		}
		s.walletState = walletAccount.State
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
	executionPayload := athcon.EncodedExecutionPayload(s.walletState, payloadEncoded)

	s.logger.Debug("executing verify", zap.Uint32("layer", s.host.Layer().Uint32()), zap.Int64("maxgas", maxgas))
	output, gasLeft, err := vmhost.Execute(
		s.host.Layer(),
		maxgas,
		s.host.Principal(),
		s.host.Principal(),
		executionPayload,
		0,
		s.templateCode,
	)

	// consume verify gas
	// TODO(lane): safe arithmetic/assumption checking
	s.host.SpendGas(uint64(maxgas - gasLeft))
	if err != nil {
		return fmt.Errorf("verifying TX: %w", err)
	}
	if len(output) == 0 {
		return errors.New("empty verify output")
	}
	s.logger.Debug("verify finished",
		zap.Int64("actual gas", maxgas-gasLeft),
		zap.Bool("valid", output[0] == 1),
	)
	if output[0] != 1 {
		return errors.New("TX didn't pass verification")
	}
	return nil
}

func (s *Wallet) BaseGas() uint64 {
	return BaseGas()
}

func (s *Wallet) LoadGas() uint64 {
	return LoadGas()
}
