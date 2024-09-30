package types

//go:generate scalegen

// StorageItem represents a single item of account storage
type StorageItem struct {
	// not actually a hash, just 32 bytes
	Key   Hash32
	Value Hash32
}

type StorageItems []StorageItem
