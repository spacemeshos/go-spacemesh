package types

// NetworkID is unique network identifier
type NetworkID [32]byte

// GetNetworkID returns current network identifier
func GetNetworkID() NetworkID {
	return NetworkID{}
}
