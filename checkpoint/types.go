package checkpoint

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type Checkpoint struct {
	Version string    `json:"version"`
	Data    InnerData `json:"data"`
}

type InnerData struct {
	CheckpointId string     `json:"id"`
	Restore      uint32     `json:"restore"`
	Atxs         []ShortAtx `json:"atxs"`
	Accounts     []Account  `json:"accounts"`
}

type ShortAtx struct {
	ID             []byte `json:"id"`
	Epoch          uint32 `json:"epoch"`
	CommitmentAtx  []byte `json:"commitmentAtx"`
	VrfNonce       uint64 `json:"vrfNonce"`
	NumUnits       uint32 `json:"numUnits"`
	BaseTickHeight uint64 `json:"baseTickHeight"`
	TickCount      uint64 `json:"tickCount"`
	PublicKey      []byte `json:"publicKey"`
	Sequence       uint64 `json:"sequence"`
	Coinbase       []byte `json:"coinbase"`
}

func (vatx ShortAtx) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("atx_id", types.BytesToHash(vatx.ID).ShortString())
	encoder.AddString("smesher", types.BytesToNodeID(vatx.PublicKey).String())
	encoder.AddString("commitment_atx_id", types.BytesToHash(vatx.CommitmentAtx).String())
	encoder.AddUint32("epoch", vatx.Epoch)
	encoder.AddUint64("sequence_number", vatx.Sequence)
	return nil
}

type Account struct {
	Address  []byte `json:"address"`
	Balance  uint64 `json:"balance"`
	Nonce    uint64 `json:"nonce"`
	Template []byte `json:"template"`
	State    []byte `json:"state"`
}
