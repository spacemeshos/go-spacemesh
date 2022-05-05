package types

// Account represents account state at a certain layer.
type Account struct {
	Layer   LayerID `json:"-"`
	Address Address `json:"-"`
	Nonce   uint64  `jons:"-"`
	Balance uint64  `json:"balance"`
}
