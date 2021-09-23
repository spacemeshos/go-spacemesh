package poet

import (
	"fmt"
	"testing"
	"time"
)

type envMock struct {
	TestInstanceCount int
}

func (env envMock) RecordMessage(msg string, a ...interface{}) {
	fmt.Println(msg, a)
}

func Test_PoetCreation(t *testing.T) {

	env := envMock{TestInstanceCount:5}

	env.RecordMessage("Getting poet ip")
	//poet_ip := netclient.MustGetDataNetworkIP()
	env.RecordMessage("starting poet")
	_, err := StartPoet(func(c *Config) {
		//['--rpclisten', '0.0.0.0:50002', '--restlisten', '0.0.0.0:80', "--n", "19"
		//c.RawRPCListener = "0.0.0.0:50002"
		//c.RawRESTListener ="0.0.0.0:80"
		c.DataDir = "poetdata"
		c.PoetDir = "poet"
		c.LogDir = "log"
		c.Service.N = 19
	})
	if err != nil {
		fmt.Printf("LOL %v", err)
		//env.RecordFailure(err)
	}
	//poetHarness := activation.NewHTTPPoetClient("0.0.0.0:80")
	//client.MustPublish(ctx, poets_topic, string(poet_ip.String()+":80"))
	//env.RecordMessage("Published poet data ", string(poet_ip.String()+":80"))
	time.Sleep(100*time.Second)
	//gatewaych := make(chan node2.Info)
	//client.MustSubscribe(ctx, gateways_topic, gatewaych)
	//gatewaylist := make([]string, 0)
	//
	//for g := range gatewaych {
	//	env.RecordMessage("Added poet gatweway %v", g.IP.String()+":9092")
	//	gatewaylist = append(gatewaylist, g.IP.String()+":9092")
}