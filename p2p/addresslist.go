package p2p

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/multiformats/go-multiaddr"
)

// AddressList represents a list of addresses.
type AddressList []multiaddr.Multiaddr

// AddressListFromStringSlice parses strings in the slice into
// Multiaddr values and returns them as an AddressList.
func AddressListFromStringSlice(s []string) (AddressList, error) {
	al := make(AddressList, len(s))
	for n, s := range s {
		ma, err := multiaddr.NewMultiaddr(s)
		if err != nil {
			return nil, fmt.Errorf("address %s is not a valid multiaddr: %w", s, err)
		}
		al[n] = ma
	}

	return al, nil
}

// AddressListFromString parses a string containing a Multiaddr
// and returns it as an AddressList.
func AddressListFromString(s string) (AddressList, error) {
	return AddressListFromStringSlice([]string{s})
}

// MustParseAddresses parses multiaddr strings into AddressList, and
// panics upon any parse errors.
func MustParseAddresses(s ...string) AddressList {
	al, err := AddressListFromStringSlice(s)
	if err != nil {
		panic(err)
	}
	return al
}

// String implements Stringer.
func (al AddressList) String() string {
	str, _ := writeAsCSV(al)
	return "[" + str + "]"
}

func writeAsCSV(vals AddressList) (string, error) {
	strs := make([]string, len(vals))
	for n, v := range vals {
		strs[n] = v.String()
	}
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	if err := w.Write(strs); err != nil {
		return "", err
	}
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n"), nil
}
