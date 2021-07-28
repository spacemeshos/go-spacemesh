package config

// GenesisConfig defines accounts that will exist in state at genesis
type GenesisConfig struct {
	Accounts map[string]uint64 `mapstructure:"accounts"`
}

// Account1Pub is the public key for testing
const Account1Pub = "0x7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

// Account1Private is the private key for test account
const Account1Private = "0x81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce57be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

// Account2Pub is the public key for second test account
const Account2Pub = "0x22a31a84ab876f82fcafba86e77910b4419a4ee0f1d5483d7dd3b5b6b6922ee9"

// Account2Private is the private key for second test account
const Account2Private = "0x9d411020d46d3f4e1214f7b51052219737669f461ac9c9ac6ac49753926d0af222a31a84ab876f82fcafba86e77910b4419a4ee0f1d5483d7dd3b5b6b6922ee9"

// DefaultGenesisConfig is the default configuration for the node
func DefaultGenesisConfig() *GenesisConfig {
	// NOTE(dshulyak) keys in default config are used in some tests
	g := GenesisConfig{}

	// we default to 10^5 SMH per account which is 10^17 smidge
	// each genesis account starts off with 10^17 smidge
	g.Accounts = map[string]uint64{
		"0x1":       100000000000000000,
		Account1Pub: 100000000000000000,
		Account2Pub: 100000000000000000,
	}
	return &g
}
