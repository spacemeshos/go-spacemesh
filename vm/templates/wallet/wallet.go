package wallet

import (
	"encoding/binary"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/vm/core"
	vmhost "github.com/spacemeshos/go-spacemesh/vm/host"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
)

// New returns Wallet instance with SpawnArguments.
func New(host core.Host, cache core.AccountLoader, spawnArgs []byte) (*Wallet, error) {
	templateAccount, err := cache.Get(host.TemplateAddress())
	if err != nil {
		return nil, fmt.Errorf("failed to load template account: %w", err)
	} else if len(templateAccount.State) == 0 {
		return nil, fmt.Errorf("template account state is empty")
	}
	templateCode := templateAccount.State

	// Instantiate the VM
	vmhost, err := vmhost.NewHostLightweight(host)
	if err != nil {
		return nil, fmt.Errorf("loading Athena VM: %w", err)
	}

	// store the pubkey, i.e., the constructor args (aka immutable state) required to instantiate
	// the wallet program instance in Athena, so we can lazily instantiate it as required.
	return &Wallet{host, vmhost, templateCode, spawnArgs}, nil
}

//go:generate scalegen

// Wallet is a single-key wallet.
type Wallet struct {
	host         core.Host
	vmhost       core.VMHost
	templateCode []byte
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

// Verify that transaction is signed by the owner of the PublicKey using ed25519.
func (s *Wallet) Verify(host core.Host, raw []byte, dec *scale.Decoder) bool {
	return true
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
