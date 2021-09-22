module github.com/spacemeshos/go-spacemesh/plans/network

go 1.16

replace github.com/spacemeshos/go-spacemesh => ../..

require (
	github.com/btcsuite/btcutil v0.0.0-20190425235716-9e5f4b9a998d
	github.com/grpc-ecosystem/grpc-gateway v1.14.6
	github.com/jessevdk/go-flags v1.4.0
	github.com/spacemeshos/go-spacemesh v0.1.45
	github.com/spacemeshos/poet v0.1.1-0.20201013202053-99eed195dc2d
	github.com/spacemeshos/post v0.0.0-20210831040706-7255a25137a2
	github.com/spacemeshos/smutil v0.0.0-20190604133034-b5189449f5c5
	github.com/testground/sdk-go v0.2.6-0.20201016180515-1e40e1b0ec3a
	go.uber.org/zap v1.15.0
	golang.org/x/net v0.0.0-20201021035429-f5854403a974
	google.golang.org/grpc v1.32.0
)
