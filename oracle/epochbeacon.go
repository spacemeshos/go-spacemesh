package oracle

import (
	"encoding/binary"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type EpochBeaconProvider struct {
}

func (p *EpochBeaconProvider) GetBeacon(epochNumber types.EpochId) []byte {
	ret := make([]byte, 32)
	binary.LittleEndian.PutUint64(ret, uint64(epochNumber))
	return ret
}
