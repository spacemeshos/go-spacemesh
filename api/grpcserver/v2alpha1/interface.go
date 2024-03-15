package v2alpha1

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"time"
)

// genesisTimeAPI is an API to get genesis time and current layer of the system.
type genesisTimeAPI interface {
	GenesisTime() time.Time
	CurrentLayer() types.LayerID
}
