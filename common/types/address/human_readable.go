package address

import (
	"errors"
)

// ErrUnsupportedNetwork is returned when a network is not supported.
var ErrUnsupportedNetwork = errors.New("unsupported network")

// GetHRPNetwork returns the Human-Readable-Part of bech32 addresses for a networkID.
func (a Address) GetHRPNetwork() string {
	return conf.NetworkHRP
}

// GetNetwork returns prefix of bech32 addresses for a networkID.
func (a Address) GetNetwork() string {
	return conf.NetworkName
}
