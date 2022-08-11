package multisig

import (
	"strconv"
	"testing"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
	"github.com/stretchr/testify/require"
)

func TestKeysLimits(t *testing.T) {
	reg := registry.New()
	Register(reg)

	spawnGas := []int{TotalGasSpawn1, TotalGasSpawn2, TotalGasSpawn3}
	for i, address := range []core.Address{TemplateAddress1, TemplateAddress2, TemplateAddress3} {
		expectedK := i + 1
		t.Run(strconv.Itoa(expectedK), func(t *testing.T) {
			for n := 0; n < StorageLimit+5; n++ {
				t.Run(strconv.Itoa(n), func(t *testing.T) {
					handler := reg.Get(address)
					args := SpawnArguments{PublicKeys: make([]core.PublicKey, n)}
					ctx := core.Context{}
					ctx.Account.Balance = 1_000_000
					ctx.Header.GasPrice = 1
					ctx.Header.MaxGas = 1_000_000
					ctx.Header.Principal = core.ComputePrincipal(address, &args)
					err := handler.Exec(&ctx, methodSpawn, &args)
					if n < expectedK || n > StorageLimit {
						require.Error(t, err)
						require.Equal(t, spawnGas[i], int(ctx.Consumed()))
					} else {
						require.NoError(t, err)
						require.Equal(t, spawnGas[i]+n*StorageCostPerKey, int(ctx.Consumed()))
					}
				})
			}
		})

	}
}
