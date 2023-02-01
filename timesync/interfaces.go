package timesync

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// clock defines the functionality needed from any clock type.
type clock interface {
	Now() time.Time
}

// layerConv provides conversions from time to layer and vice versa.
type layerConv interface {
	TimeToLayer(time.Time) types.LayerID
	LayerToTime(types.LayerID) time.Time
}
