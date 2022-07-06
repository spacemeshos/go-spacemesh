package address

import (
	"errors"
)

// Network is the networkID of a bech32 address.
type Network uint8

const (
	// HumanReadablePartLength length address string in bytes with HumanReadablePart.
	HumanReadablePartLength = 1

	// MainnetID is the networkID of the mainnet.
	MainnetID Network = 1
	// TestnetID is the networkID of the testnet.
	TestnetID Network = 2

	// MainnetName is the prefix of the mainnet in address.
	MainnetName = "sm"
	// TestnetName is the prefix of the testnet in address.
	TestnetName = "smt"

	// MainnetHRP is the Human-Readable-Part of the mainnet in address.
	MainnetHRP = "spacemesh main name"
	// TestnetHRP is the Human-Readable-Part of the testnet in address.
	TestnetHRP = "testnet"
)

var (
	// ErrUnsupportedNetwork is returned when a network is not supported.
	ErrUnsupportedNetwork = errors.New("unsupported network")

	networkIDToNetworkName = map[Network]string{
		MainnetID: MainnetName,
		TestnetID: TestnetName,
	}
	networkNameToNetworkID = map[string]Network{
		MainnetName: MainnetID,
		TestnetName: TestnetID,
	}

	networkIDToHRP = map[Network]string{
		MainnetID: MainnetHRP,
		TestnetID: TestnetHRP,
	}
	networkHRPToNetworkID = map[string]Network{
		MainnetHRP: MainnetID,
		TestnetHRP: TestnetID,
	}
)

// HRPData is the Human-Readable-Part of bech32 addresses.
type HRPData [HumanReadablePartLength]byte

// NewHRPDataFromName returns the Human-Readable-Part of bech32 addresses for a networkName.
func NewHRPDataFromName(networkName string) (*HRPData, error) {
	if hrpID, ok := networkNameToNetworkID[networkName]; ok {
		var addr HRPData
		addr[0] = byte(hrpID)
		return &addr, nil
	}
	return nil, ErrUnsupportedNetwork
}

// NewHRPDataFromNetwork returns the Human-Readable-Part of bech32 addresses for a networkID.
func NewHRPDataFromNetwork(networkID Network) (*HRPData, error) {
	if _, ok := networkIDToNetworkName[networkID]; ok {
		var addr HRPData
		addr[0] = byte(networkID)
		return &addr, nil
	}
	return nil, ErrUnsupportedNetwork
}

// NewHRPDataFromBytes returns the Human-Readable-Part of bech32 addresses for a networkName.
func NewHRPDataFromBytes(bytes []byte) (string, error) {
	if len(bytes) == 0 {
		return "", errors.New("empty bytes")
	}
	if networkName, ok := networkIDToNetworkName[Network(bytes[0])]; ok {
		return networkName, nil
	}
	return "", ErrUnsupportedNetwork
}

// GetNetworkID returns the networkID of a bech32 address.
func (a Address) GetNetworkID() (Network, error) {
	if _, ok := networkIDToNetworkName[Network(a[0])]; ok {
		return Network(a[0]), nil
	}
	return 0, ErrUnsupportedNetwork
}

// GetHRPNetwork returns the Human-Readable-Part of bech32 addresses for a networkID.
func (a Address) GetHRPNetwork() (string, error) {
	if hrp, ok := networkIDToHRP[Network(a[0])]; ok {
		return hrp, nil
	}
	return "", ErrUnsupportedNetwork
}

// GetNetwork returns prefix of bech32 addresses for a networkID.
func (a Address) GetNetwork() (string, error) {
	if prefix, ok := networkIDToNetworkName[Network(a[0])]; ok {
		return prefix, nil
	}
	return "", ErrUnsupportedNetwork
}
