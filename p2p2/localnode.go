package p2p2

type LocalNode interface {
	Id() []byte
	String() string
	Pretty() string

	PrivateKey() PrivateKey
	PublicKey() PublicKey
}
