package v2alpha1

//go:generate mockgen -typed -package=v2alpha1 -destination=./mocks.go -source=./interface.go

type malfeasanceInfo interface {
	Info(data []byte) (map[string]string, error)
}
