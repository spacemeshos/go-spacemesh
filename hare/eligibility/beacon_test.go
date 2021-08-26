package eligibility

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/stretchr/testify/require"
)

type mockBeaconProvider struct {
	value []byte
}

func (mbp mockBeaconProvider) GetBeacon(types.EpochID) ([]byte, error) {
	return mbp.value, nil
}

// TODO(nkryuchkov): enable when beacon sync is finished
func TestBeacon_Value(t *testing.T) {
	t.Skip()

	r := require.New(t)

	b := NewBeacon(nil, 0, logtest.New(t))
	c := newMockCacher()
	b.cache = c

	beaconValue := []byte{1, 2, 3, 4}
	b.beaconGetter = &mockBeaconProvider{beaconValue}
	b.confidenceParam = cfg.ConfidenceParam
	val, err := b.Value(context.TODO(), 100)
	r.NoError(err)
	r.Equal(binary.LittleEndian.Uint32(beaconValue), val)
	r.Equal(1, c.numGet)
	r.Equal(1, c.numAdd)

	// test cache
	val, err = b.Value(context.TODO(), 100)
	r.NoError(err)
	r.Equal(2, c.numGet)
	r.Equal(1, c.numAdd)

	val, err = b.Value(context.TODO(), 1)
	r.NoError(err)
}

func TestNewBeacon(t *testing.T) {
	r := require.New(t)
	p := &mockBeaconProvider{}
	b := NewBeacon(p, 10, logtest.New(t))
	r.Equal(p, b.beaconGetter)
	r.Equal(10, int(b.confidenceParam))
	r.NotNil(p, b.cache)
}
