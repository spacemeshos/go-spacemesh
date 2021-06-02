package weakcoin

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Message defines weak coin message format.
type Message struct {
	Epoch     types.EpochID
	Round     types.RoundID
	Proposal  []byte
	Signature []byte
}

func (w Message) String() string {
	return fmt.Sprintf("%v/%v/%v", w.Epoch, w.Round, w.Proposal)
}
