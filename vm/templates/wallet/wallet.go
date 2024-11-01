package wallet

import (
	"encoding/binary"
	"errors"
	"fmt"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/vm/core"
	vmhost "github.com/spacemeshos/go-spacemesh/vm/host"
)

// New returns Wallet instance with SpawnArguments.
func New(host core.Host) (*Wallet, error) {
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

	return &Wallet{host, templateCode, walletState}, nil
}

// Wallet is a single-key wallet.
type Wallet struct {
	host         core.Host
	templateCode []byte
	walletState  []byte
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
	vmhost, err := vmhost.NewHost(host)
	if err != nil {
		return 0, fmt.Errorf("loading Athena VM: %w", err)
	}

	output, _, err := vmhost.Execute(
		s.host.Layer(),
		maxgas,
		s.host.Principal(),
		s.host.Principal(),
		executionPayload,
		0,
		s.templateCode,
	)
	maxspend := binary.LittleEndian.Uint64(output)
	return maxspend, err
}

// Verify the transaction signature using the VM.
func (s *Wallet) Verify(raw []byte, dec *scale.Decoder) bool {
	sig := core.Signature{}
	n, err := sig.DecodeScale(dec)
	if err != nil {
		return false
	}

	// deconstruct the tx, temporarily removing the signature, and add the genesis ID
	// TODO(lane): re-add support for genesisID
	// see https://github.com/athenavm/athena/issues/178
	// rawTx := core.SigningBody(host.GetGenesisID().Bytes(), raw[:len(raw)-n])
	rawTx := raw[:len(raw)-n]
	// reconstruct, with the signature
	// methodArgs := append(rawTx, sig[:]...)

	// The input to the verify method must be SCALE-encoded.
	verifyArgsEncoded, err := gossamerScale.Marshal(struct {
		RawTx []byte
		Sig   [64]byte
	}{rawTx, sig})
	if err != nil {
		return false
	}

	maxgas := int64(s.host.MaxGas())
	if maxgas < 0 {
		return false
	}

	// Instantiate the VM
	// Use a mock host to ensure that no state changes occur.
	host := s.host.Clone()
	vmhost, err := vmhost.NewHost(host)
	if err != nil {
		return false
	}

	// If this is a spawn transaction, the wallet state is currently empty. So we need to
	// provisionally spawn the wallet program instance so we can call the verify method.
	if s.host.IsSpawn() {
		if len(s.walletState) != 0 {
			// TODO(lane): should we allow spawn to be called multiple times on the same account?
			return false
		}

		// the transaction must already be a spawn tx, so there's no need to modify the payload.
		executionPayload := athcon.EncodedExecutionPayload(nil, s.host.Payload())
		_, _, err = vmhost.Execute(
			s.host.Layer(),
			maxgas,
			s.host.Principal(),
			s.host.Principal(),
			executionPayload,
			0,
			s.templateCode,
		)
		if err != nil {
			return false
		}

		// the account should've been spawned
		walletAccount, err := host.Get(s.host.Principal())
		if err != nil {
			return false
		}
		if len(walletAccount.State) == 0 {
			// this should not happen!
			return false
		}
		s.walletState = walletAccount.State
	}

	// construct the payload: wallet state + payload (method selector + input (raw tx + signature))
	verifySelector, _ := athcon.FromString("athexp_verify")
	payload := athcon.Payload{
		Selector: &verifySelector,
		Input:    verifyArgsEncoded,
	}
	payloadEncoded, err := gossamerScale.Marshal(payload)
	if err != nil {
		return false
	}
	executionPayload := athcon.EncodedExecutionPayload(s.walletState, payloadEncoded)

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
	s.host.SpendGas(uint64(maxgas) - uint64(gasLeft))

	return err == nil && len(output) == 1 && output[0] == 1
}

func (s *Wallet) BaseGas() uint64 {
	return BaseGas()
}

func (s *Wallet) LoadGas() uint64 {
	return LoadGas()
}
