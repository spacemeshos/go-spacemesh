package eligibility

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

func TestBeacon_Value(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	epoch := types.EpochID(100)
	beaconValue := []byte{1, 2, 3, 4}
	mb := smocks.NewMockBeaconGetter(ctrl)
	mb.EXPECT().GetBeacon(epoch).Return(beaconValue, nil).Times(1)
	encodedValue := uint32(0x4030201) // binary.LittleEndian.Uint32(beaconValue)
	b := NewBeacon(mb, logtest.New(t))

	val, err := b.Value(context.TODO(), 100)
	assert.NoError(t, err)
	assert.Equal(t, encodedValue, val)
}
