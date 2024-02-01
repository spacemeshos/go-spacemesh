package mapstructureutil

import (
	"reflect"

	"github.com/mitchellh/mapstructure"

	"github.com/spacemeshos/go-spacemesh/p2p"
)

// AddressListDecodeFunc mapstructure decode func for p2p.AddressList.
// AddressList can be represented either by a string or by a slice of strings.
func AddressListDecodeFunc() mapstructure.DecodeHookFunc {
	return func(f, t reflect.Type, data any) (any, error) {
		if t != reflect.TypeOf(p2p.AddressList(nil)) {
			return data, nil
		}

		switch f.Kind() {
		case reflect.Slice:
			v := reflect.ValueOf(data)
			items := make([]string, v.Len())
			for n := 0; n < v.Len(); n++ {
				item := v.Index(n).Interface()
				switch s := item.(type) {
				case string:
					items[n] = s
				default:
					return data, nil
				}
			}
			r, err := p2p.AddressListFromStringSlice(items)
			return r, err
		case reflect.String:
			r, err := p2p.AddressListFromString(data.(string))
			return r, err
		default:
			return data, nil
		}
	}
}
