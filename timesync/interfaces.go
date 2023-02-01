package timesync

import (
	"time"
)

// clock defines the functionality needed from any clock type.
type clock interface {
	Now() time.Time
}
