package types

// Account represents account state at a certain layer.
type Account struct {
	Layer   LayerID
	Address Address
	Nonce   uint64
	Balance uint64
}
