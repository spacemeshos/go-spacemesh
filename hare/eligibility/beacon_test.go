package eligibility

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

type mockBeaconProvider struct {
	value []byte
}

func (mbp mockBeaconProvider) GetBeacon(types.EpochID) ([]byte, error) {
	return mbp.value, nil
}

func TestBeacon_Value(t *testing.T) {
	beaconValue := []byte{1, 2, 3, 4}
	encodedValue := uint32(67305985) // binary.LittleEndian.Uint32(beaconValue)
	b := NewBeacon(&mockBeaconProvider{beaconValue}, logtest.New(t))

	val, err := b.Value(context.TODO(), 100)
	assert.NoError(t, err)
	assert.Equal(t, encodedValue, val)
}
