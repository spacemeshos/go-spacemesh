package templates

import (
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
)

var BuySelector, DeploySelector, MaxSpendSelector, ProxySelector, SpawnSelector, SpendSelector athcon.MethodSelector

func init() {
	var err error
	BuySelector, err = athcon.FromString("athexp_buy")
	if err != nil {
		panic(err.Error())
	}
	DeploySelector, err = athcon.FromString("athexp_deploy")
	if err != nil {
		panic(err.Error())
	}
	MaxSpendSelector, err = athcon.FromString("athexp_max_spend")
	if err != nil {
		panic(err.Error())
	}
	ProxySelector, err = athcon.FromString("athexp_proxy")
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
