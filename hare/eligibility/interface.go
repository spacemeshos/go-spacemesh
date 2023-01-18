package eligibility

//go:generate mockgen -package=eligibility -destination=./mocks.go -source=./interface.go

type cache interface {
	Add(key, value any) (evicted bool)
	Get(key any) (value any, ok bool)
}
