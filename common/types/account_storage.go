package types

//go:generate scalegen

// StorageItem represents a single item of account storage.
type StorageItem struct {
	Key   [32]byte
	Value [32]byte
}
