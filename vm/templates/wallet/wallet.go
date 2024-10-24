package wallet

import (
	"encoding/binary"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/vm/core"
	vmhost "github.com/spacemeshos/go-spacemesh/vm/host"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
)

// New returns Wallet instance with SpawnArguments.
func New(host core.Host, cache core.AccountLoader, spawnArgs []byte) (*Wallet, error) {
	// Load the template account
	templateAccount, err := cache.Get(host.TemplateAddress())
	if err != nil {
		return nil, fmt.Errorf("failed to load template account: %w", err)
	} else if len(templateAccount.State) == 0 {
		return nil, fmt.Errorf("template account state is empty")
	}
	templateCode := templateAccount.State

	// Load the wallet state
	walletAccount, err := cache.Get(host.Principal())
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet principal account: %w", err)
	} else if len(walletAccount.State) == 0 {
		return nil, fmt.Errorf("wallet account state is empty")
	}
	walletState := walletAccount.State

	// Instantiate the VM
	vmhost, err := vmhost.NewHostLightweight(host)
	if err != nil {
		return nil, fmt.Errorf("loading Athena VM: %w", err)
	}

	// store the pubkey, i.e., the constructor args (aka immutable state) required to instantiate
	// the wallet program instance in Athena, so we can lazily instantiate it as required.
	return &Wallet{host, vmhost, templateCode, walletState, spawnArgs}, nil
}

//go:generate scalegen

// Wallet is a single-key wallet.
type Wallet struct {
	host         core.Host
	vmhost       core.VMHost
	templateCode []byte
	walletState  []byte
	spawnArgs    []byte
}

// MaxSpend returns amount specified in the SpendArguments for Spend method.
func (s *Wallet) MaxSpend(spendArgs []byte) (uint64, error) {
	maxgas := int64(s.host.MaxGas())
	if maxgas < 0 {
		return 0, fmt.Errorf("gas limit exceeds maximum int64 value")
	}

	// Make sure we have a method selector
	if len(spendArgs) < 4 {
		return 0, fmt.Errorf("spendArgs is too short")
	}

	// Check the method selector
	// We define MaxSpend for any method other than spend to be zero for now.
	spendSelector, _ := athcon.FromString("athexp_spend")
	txSelector := athcon.MethodSelector(spendArgs[:athcon.MethodSelectorLength])
	if spendSelector != txSelector {
		return 0, nil
	}

	// construct the payload. this requires some surgery to replace the method maxSpendSelector.
	maxSpendSelector, _ := athcon.FromString("athexp_max_spend")
	maxGasPayload := append(maxSpendSelector[:], spendArgs[4:]...)

	output, _, err := s.vmhost.Execute(
		s.host.Layer(),
		maxgas,
		s.host.Principal(),
		s.host.Principal(),
		maxGasPayload,
		0,
		s.templateCode,
	)
	maxspend := binary.LittleEndian.Uint64(output)
	return maxspend, err
}

// Verify the transaction signature using the VM
func (s *Wallet) Verify(host core.Host, raw []byte, dec *scale.Decoder) bool {
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

	fmt.Println("running!")
	output, _, err := s.vmhost.Execute(
		s.host.Layer(),
		maxgas,
		s.host.Principal(),
		s.host.Principal(),
		executionPayload,
		0,
		s.templateCode,
	)
	return err == nil && len(output) == 1 && output[0] == 1
}

func (s *Wallet) BaseGas() uint64 {
	return BaseGas()
}

func (s *Wallet) LoadGas() uint64 {
	return LoadGas()
}

func (s *Wallet) ExecGas() uint64 {
	return ExecGas()
}
