package flags

import (
	"encoding/csv"
	"strings"

	"github.com/spacemeshos/go-spacemesh/p2p"
)

// AddressListValue wraps an AddressList to make it possible to use it
// like a slice value in pflag.
type AddressListValue struct {
	value   *p2p.AddressList
	changed bool
}

// NewAddressListValue creates an AddressListValue that wraps the
// specified AddressList.
func NewAddressListValue(val p2p.AddressList, p *p2p.AddressList) *AddressListValue {
	alv := new(AddressListValue)
	alv.value = p
	*alv.value = val
	return alv
}

// Set implements pflag.Value.
func (alv *AddressListValue) Set(val string) error {
	v, err := readAsCSV(val)
	if err != nil {
		return err
	}
	if !alv.changed {
		*alv.value = v
	} else {
		*alv.value = append(*alv.value, v...)
	}
	alv.changed = true
	return nil
}

// String implements pflag.Value.
func (alv *AddressListValue) String() string {
	return alv.value.String()
}

// Type implements pflag.Value.
func (alv *AddressListValue) Type() string {
	return "addressList"
}

// Append implements pflag.SliceValue.
func (alv *AddressListValue) Append(val string) error {
	al, err := p2p.AddressListFromString(val)
	if err != nil {
		return err
	}
	*alv.value = append(*alv.value, al...)
	return nil
}

// GetSlice implements pflag.SliceValue.
func (alv *AddressListValue) GetSlice() []string {
	r := make([]string, len(*alv.value))
	for n, ma := range *alv.value {
		r[n] = ma.String()
	}
	return r
}

// Replace implements pflag.SliceValue.
func (alv *AddressListValue) Replace(val []string) error {
	al, err := p2p.AddressListFromStringSlice(val)
	if err != nil {
		return err
	}
	*alv.value = al
	return nil
}

func readAsCSV(val string) (p2p.AddressList, error) {
	if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
		val = val[1 : len(val)-1]
	}
	if val == "" {
		return p2p.AddressList{}, nil
	}
	stringReader := strings.NewReader(val)
	csvReader := csv.NewReader(stringReader)
	l, err := csvReader.Read()
	if err != nil {
		return nil, err
	}
	return p2p.AddressListFromStringSlice(l)
}
