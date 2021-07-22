module github.com/spacemeshos/go-spacemesh/systest

go 1.16

replace github.com/spacemeshos/go-spacemesh => ../

require (
	github.com/spacemeshos/api/release/go v0.0.0-20210716063128-3a1508b28f2c
	github.com/spacemeshos/go-spacemesh v0.1.44
	github.com/testground/sdk-go v0.2.6-0.20201016180515-1e40e1b0ec3a
	google.golang.org/grpc v1.32.0
)
