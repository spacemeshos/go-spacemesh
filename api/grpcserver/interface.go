package grpcserver

import "context"

//go:generate mockgen -package=grpcserver -destination=./mocks.go -source=./interface.go

type txValidator interface {
	VerifyAndCacheTx(context.Context, []byte) error
}
