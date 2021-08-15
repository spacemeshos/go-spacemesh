package config

// GenesisConfig defines accounts that will exist in state at genesis
type GenesisConfig struct {
	Accounts map[string]uint64 `mapstructure:"accounts"`
}

// Account1Address is the address from Account1Private
const Account1Address = "0x1b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

// Account1Private is the private key for test account
const Account1Private = "0x81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce57be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

// Account2Address is the address from Account2Address
const Account2Address = "0xe77910b4419a4ee0f1d5483d7dd3b5b6b6922ee9"

// Account2Private is the private key for secode test account
const Account2Private = "0x9d411020d46d3f4e1214f7b51052219737669f461ac9c9ac6ac49753926d0af222a31a84ab876f82fcafba86e77910b4419a4ee0f1d5483d7dd3b5b6b6922ee9"

// DefaultGenesisConfig is the default configuration for the node
func DefaultGenesisConfig() *GenesisConfig {
	// NOTE(dshulyak) keys in default config are used in some tests
	g := GenesisConfig{}

	// we default to 10^5 SMH per account which is 10^17 smidge
	// each genesis account starts off with 10^17 smidge
	g.Accounts = map[string]uint64{
		Account1Address: 100000000000000000,
		Account2Address: 100000000000000000,
	}
	return &g
}
