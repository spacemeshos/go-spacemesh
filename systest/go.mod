module github.com/spacemeshos/go-spacemesh/systest

go 1.16

replace github.com/spacemeshos/go-spacemesh => ../

require (
	github.com/spacemeshos/api/release/go v1.3.1-0.20210726064428-9474f58a4ace
	github.com/spacemeshos/ed25519 v0.0.0-20190530014421-e235766d15a1
	github.com/spacemeshos/go-spacemesh v0.1.44
	github.com/testground/sdk-go v0.2.6-0.20201016180515-1e40e1b0ec3a
	google.golang.org/grpc v1.32.0
)
