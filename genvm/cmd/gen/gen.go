package gen

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/spacemeshos/economics/constants"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vault"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vesting"
)

type Input struct {
	Keys     []string `json:"keys"`
	Required int      `json:"required"`
	Total    float64  `json:"total"`
}

type Output struct {
	Debug struct {
		VestingAddress string
		VestingArgs    multisig.SpawnArguments
		VaultAddress   string
		VaultArgs      vault.SpawnArguments
	} `json:"-"`

	Address string `json:"address"`
	Balance uint64 `json:"balance"`
}

func Generate(input Input) Output {
	var keys []core.PublicKey
	for _, data := range input.Keys {
		keys = append(keys, decodeHexKey([]byte(data)))
	}
	vestingArgs := &multisig.SpawnArguments{
		Required:   uint8(input.Required),
		PublicKeys: keys,
	}
	if int(vestingArgs.Required) > len(vestingArgs.PublicKeys) {
		fmt.Printf("requires more signatures (%d) then public keys (%d) in the wallet\n",
			vestingArgs.Required,
			len(vestingArgs.PublicKeys),
		)
		os.Exit(1)
	}
	vestingAddress := core.ComputePrincipal(vesting.TemplateAddress, vestingArgs)
	vaultArgs := &vault.SpawnArguments{
		Owner:               vestingAddress,
		TotalAmount:         uint64(input.Total),
		InitialUnlockAmount: uint64(input.Total) / 4,
		VestingStart:        types.LayerID(constants.VestStart),
		VestingEnd:          types.LayerID(constants.VestEnd),
	}
	vaultAddress := core.ComputePrincipal(vault.TemplateAddress, vaultArgs)
	output := Output{
		Address: vaultAddress.String(),
		Balance: uint64(input.Total),
	}
	output.Debug.VestingAddress = vestingAddress.String()
	output.Debug.VestingArgs = *vestingArgs
	output.Debug.VaultAddress = vaultAddress.String()
	output.Debug.VaultArgs = *vaultArgs
	return output
}

func decodeHexKey(data []byte) [ed25519.PublicKeySize]byte {
	data = bytes.Trim(data, "\n")
	key := [ed25519.PublicKeySize]byte{}
	n, err := hex.Decode(key[:], data)
	must(err)
	if n != len(key) {
		must(fmt.Errorf("key %s can't be decoded from hex into %d bytes", key, len(key)))
	}
	return key
}

func must(err error) {
	if err != nil {
		fmt.Println("fatal error: ", err.Error())
		os.Exit(1)
	}
}
