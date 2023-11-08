package types

//go:generate scalegen

type PoetServiceID struct {
	ServiceID []byte `scale:"max=32"` // public key of the PoET service
}
