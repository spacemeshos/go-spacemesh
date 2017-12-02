package crypto

import (
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
)

// An Id derived from any string
// Used to for node ids and account ids and may be used by other types
type Id struct {
	peer.ID
}

func (id *Id) String() string {
	return peer.IDB58Encode(id.ID)
}

// create a new ID from a b58 encoded string
func NewID(b58 string) (*Id, error) {
	id, err := peer.IDB58Decode(b58)
	return &Id{id}, err
}
