package mint

import (
	"encoding/binary"
	"slices"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

var DISTRIBUTION_KEY = [32]byte{}

func DistributedTokens(account *types.Account) uint64 {
	i := slices.IndexFunc(account.Storage, func(i types.StorageItem) bool { return i.Key == DISTRIBUTION_KEY })
	if i == -1 {
		return 0
	}
	return binary.LittleEndian.Uint64(account.Storage[i].Value[:8])
}
