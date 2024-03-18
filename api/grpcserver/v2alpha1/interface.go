package v2alpha1

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// genesisTimeAPI is an API to get genesis time and current layer of the system.
type genesisTimeAPI interface {
	GenesisTime() time.Time
	CurrentLayer() types.LayerID
}
