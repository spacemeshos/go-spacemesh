package layer

import "github.com/spacemeshos/go-spacemesh/common"

type Id uint64

func (l Id) ToBytes() []byte { return common.Uint64ToBytes(uint64(l)) }
