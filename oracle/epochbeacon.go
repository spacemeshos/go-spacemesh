package oracle

import "encoding/binary"

type EpochBeaconProvider struct {
}

func (p *EpochBeaconProvider) GetBeacon(epochNumber uint64) []byte {
	ret := make([]byte, 32)
	binary.LittleEndian.PutUint64(ret, epochNumber)
	return ret
}
