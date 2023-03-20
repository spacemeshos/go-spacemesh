package bootstrap

import (
	"context"
	"net/url"
)

//go:generate mockgen -package=bootstrap -destination=./mocks.go -source=./interface.go

type Receiver interface {
	OnBoostrapUpdate(*VerifiedUpdate)
}

type httpclient interface {
	Query(context.Context, *url.URL) ([]byte, error)
}
