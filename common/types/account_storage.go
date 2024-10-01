package types

import (
	"github.com/spacemeshos/go-scale"
)

//go:generate scalegen

// StorageItem represents a single item of account storage
type StorageItem struct {
	// not actually a hash, just 32 bytes
	Key   Hash32
	Value Hash32
}

type StorageItems []StorageItem

const STORAGE_LIMIT = 1000

// go:generate doesn't generate these automatically, probably because it's a slice type.

// EncodeScale implements the scale.Encodable interface for StorageItems.
func (s StorageItems) EncodeScale(enc *scale.Encoder) (total int, err error) {
	return scale.EncodeStructSliceWithLimit(enc, s, STORAGE_LIMIT)
}

// DecodeScale implements the scale.Decodable interface for StorageItems.
func (s *StorageItems) DecodeScale(dec *scale.Decoder) (total int, err error) {
	v, total, err := scale.DecodeStructSliceWithLimit[StorageItem](dec, STORAGE_LIMIT)
	if err != nil {
		return 0, err
	}
	*s = v
	return total, err
}
