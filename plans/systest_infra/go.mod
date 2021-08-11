module github.com/spacemeshos/go-spacemesh/plans/systest_infra

go 1.16

replace github.com/spacemeshos/go-spacemesh => ../..

replace github.com/spacemeshos/go-spacemesh/systest => ../../systest

require (
	github.com/spacemeshos/go-spacemesh v0.1.45 // indirect
	github.com/spacemeshos/go-spacemesh/systest v0.0.0-00010101000000-000000000000
	github.com/testground/sdk-go v0.2.6-0.20201016180515-1e40e1b0ec3a
)
