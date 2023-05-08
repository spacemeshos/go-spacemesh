package checkpoint

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
	ID             string `json:"id"`
	Epoch          uint32 `json:"epoch"`
	CommitmentAtx  string `json:"commitmentAtx"`
	VrfNonce       uint64 `json:"vrfNonce"`
	NumUnits       uint32 `json:"numUnits"`
	BaseTickHeight uint64 `json:"baseTickHeight"`
	TickCount      uint64 `json:"tickCount"`
	PublicKey      string `json:"publicKey"`
	Sequence       uint64 `json:"sequence"`
	Coinbase       string `json:"coinbase"`
}

type Account struct {
	Address  string `json:"address"`
	Balance  uint64 `json:"balance"`
	Nonce    uint64 `json:"nonce"`
	Template string `json:"template"`
	State    string `json:"state"`
}
