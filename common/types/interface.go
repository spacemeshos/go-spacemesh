package types

type keyExtractor interface {
	ExtractNodeID(msg, sig []byte) (NodeID, error)
}

type signer interface {
	Sign([]byte) []byte
	NodeID() NodeID
}
