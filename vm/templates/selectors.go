package templates

import (
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
)

var DeploySelector, SpawnSelector, SpendSelector athcon.MethodSelector

func init() {
	var err error
	DeploySelector, err = athcon.FromString("athexp_deploy")
	if err != nil {
		panic(err.Error())
	}
	SpawnSelector, err = athcon.FromString("athexp_spawn")
	if err != nil {
		panic(err.Error())
	}
	SpendSelector, err = athcon.FromString("athexp_spend")
	if err != nil {
		panic(err.Error())
	}
}
