package main

import (
	"fmt"
	"testing"
)

type envMock struct {
	TestInstanceCount int
}

func (env envMock) RecordMessage(msg string, a ...interface{}) {
	fmt.Println(msg, a)
}
func Test_RoleAllocation(t *testing.T) {

	env := envMock{TestInstanceCount:5}

	poets := 1
	gateways := 1

	ra := &roleAllocator{}

	ra.Add("poet", poets, func(r *role) {
		env.RecordMessage("hello im poet")
		r.storage = string("LOL")
	}, func(obj interface{}) {
		env.RecordMessage("done poet ", obj.(string))
	})

	ra.Add("gateway", gateways, func(r *role) {
		env.RecordMessage("hello im gateways")
		r.storage = string("LOL")
	}, func(obj interface{}) {
		env.RecordMessage("done gateways %v", obj.(string))
	})

	ra.Add("miner", env.TestInstanceCount-poets-gateways, func(r *role) {
		env.RecordMessage("hello im miner")
		r.storage = string("LOL")
	}, func(obj interface{}) {
		env.RecordMessage("done miner %v", obj.(string))
	})

	ra.Allocate(1)
}