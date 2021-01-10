package types

type NetworkID [32]byte

func GetNetworkID() NetworkID {
	return NetworkID{}
}
