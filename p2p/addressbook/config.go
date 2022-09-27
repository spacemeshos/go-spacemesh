package addressbook

import "time"

// Config is the configuration for the address book.
type Config struct {
	// DataDir path to directory where store files
	DataDir string

	// NeedAddressThreshold is the number of addresses under which the
	// address manager will claim to need more addresses.
	NeedAddressThreshold int

	// TriedBucketSize is the maximum number of addresses in each tried address bucket.
	TriedBucketSize int

	// TriedBucketCount is the number of buckets we split tried addresses over.
	TriedBucketCount uint64

	// NewBucketSize is the maximum number of addresses in each new address bucket.
	NewBucketSize int

	// NewBucketCount is the number of buckets that we spread new addresses over.
	NewBucketCount uint64

	// TriedBucketsPerGroup is the number of tried buckets over which an address group will be spread.
	TriedBucketsPerGroup uint64

	// NewBucketsPerGroup is the number of new buckets over which an source address group will be spread.
	NewBucketsPerGroup uint64

	// NewBucketsPerAddress is the number of buckets a frequently seen new address may end up in.
	NewBucketsPerAddress int

	// NumMissingDays is the number of days before which we assume an
	// address has vanished if we have not seen it announced  in that long.
	NumMissingDays time.Duration

	// NumRetries is the number of tried without a single success before
	// we assume an address is bad.
	NumRetries int

	// MaxFailures is the maximum number of failures we will accept without
	// a success before considering an address bad.
	MaxFailures int

	// MinBadDays is the number of days since the last success before we
	// will consider evicting an address.
	MinBadDays time.Duration

	// GetAddrMax is the most addresses that we will send in response
	// to a getAddr (in practice the most addresses we will return from a
	// call to AddressCache()).
	GetAddrMax int

	// GetAddrPercent is the percentage of total addresses known that we
	// will share with a call to AddressCache.
	GetAddrPercent int

	// AnchorPeersCount is the number of anchor peers to use when node starting.
	AnchorPeersCount int

	// Size of the evicted peers cache. Controls how many evicted peers are remembered.
	RemovedPeersCacheSize int
}

// DefaultAddressBookConfigWithDataDir returns a default configuration for the address book.
func DefaultAddressBookConfigWithDataDir(dataDir string) *Config {
	return &Config{
		DataDir:               dataDir,
		NeedAddressThreshold:  1000,
		TriedBucketSize:       256,
		TriedBucketCount:      64,
		NewBucketSize:         120,
		NewBucketCount:        1024,
		TriedBucketsPerGroup:  8,
		NewBucketsPerGroup:    64,
		NewBucketsPerAddress:  8,
		NumMissingDays:        30,
		NumRetries:            3,
		MaxFailures:           10,
		MinBadDays:            7,
		GetAddrMax:            300,
		GetAddrPercent:        23,
		AnchorPeersCount:      3,
		RemovedPeersCacheSize: 64,
	}
}
