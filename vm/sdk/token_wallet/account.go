package tokenwallet

import (
	"encoding/binary"
	"slices"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Balance(account *types.Account, mint types.Address) uint64 {
	var tokenID [32]byte
	copy(tokenID[:24], mint[:])
	i := slices.IndexFunc(account.Storage, func(i types.StorageItem) bool { return i.Key == tokenID })
	if i == -1 {
		return 0
	}
	return binary.LittleEndian.Uint64(account.Storage[i].Value[:8])
}
