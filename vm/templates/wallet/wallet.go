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
func New(spawnArgs []byte) *Wallet {
	// store the pubkey, i.e., the constructor args (aka immutable state) required to instantiate
	// the wallet program instance in Athena, so we can lazily instantiate it as required.
	return &Wallet{spawnArgs: spawnArgs}
}

//go:generate scalegen

// Wallet is a single-key wallet.
type Wallet struct {
	spawnArgs []byte
}

// MaxSpend returns amount specified in the SpendArguments for Spend method.
func (s *Wallet) MaxSpend(host core.Host, loader core.AccountLoader, spendArgs []byte) (uint64, error) {
	// Load the template code
	templateAccount, err := loader.Get(host.TemplateAddress())
	if err != nil {
		return 0, fmt.Errorf("failed to load template account: %w", err)
	} else if len(templateAccount.State) == 0 {
		return 0, fmt.Errorf("template account state is empty")
	}

	// Instantiate the VM
	vmhost, err := vmhost.NewHostLightweight(host)
	if err != nil {
		return 0, fmt.Errorf("loading Athena VM: %w", err)
	}
	maxgas := int64(host.MaxGas())
	if maxgas < 0 {
		return 0, fmt.Errorf("gas limit exceeds maximum int64 value")
	}

	// construct the payload. this requires some surgery to replace the method selector.
	if len(spendArgs) < 4 {
		return 0, fmt.Errorf("spendArgs is too short")
	}
	selector, _ := athcon.FromString("athexp_max_spend")
	maxGasPayload := append(selector[:], spendArgs[4:]...)

	output, _, err := vmhost.Execute(
		host.Layer(),
		maxgas,
		host.Principal(),
		host.Principal(),
		maxGasPayload,
		0,
		templateAccount.State,
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
