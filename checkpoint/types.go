package checkpoint

type Checkpoint struct {
	Version string    `json:"version"`
	Data    InnerData `json:"data"`
}

type InnerData struct {
	CheckpointId string     `json:"id"`
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

type Account struct {
	Address  []byte `json:"address"`
	Balance  uint64 `json:"balance"`
	Nonce    uint64 `json:"nonce"`
	Template []byte `json:"template"`
	State    []byte `json:"state"`
}
