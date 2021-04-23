package discovery

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type mockDisc struct {
	pingres     error
	findnoderes []*node.Info
	findnoderr  error
}

func (md *mockDisc) Ping(ctx context.Context, key p2pcrypto.PublicKey) error {
	return md.pingres
}

func (md *mockDisc) GetAddresses(context.Context, p2pcrypto.PublicKey) ([]*node.Info, error) {
	return md.findnoderes, md.findnoderr
}

func (md *mockDisc) SetLocalAddresses(tcp, udp int) {

}

func Test_newRefresher(t *testing.T) {
	bootnodes := generateDiscNodes(10)
	cfg := config.DefaultConfig()
	//for _, b := range bootnodes {
	//	cfg.SwarmConfig.BootstrapNodes = append(cfg.SwarmConfig.BootstrapNodes, b.String())
	//}
	local := generateDiscNode()
	addrbk := newAddrBook(cfg.SwarmConfig, "", GetTestLogger("test.newRefresher.addrbook"))
	ref := newRefresher(local.PublicKey(), addrbk, &mockDisc{}, bootnodes, GetTestLogger("test.newRefresher"))

	require.Equal(t, ref.bootNodes, bootnodes)
}

func Test_expire(t *testing.T) {

	initial := time.Now()
	m := make(map[p2pcrypto.PublicKey]time.Time)
	for _, n := range generateDiscNodes(99) {
		m[n.PublicKey()] = initial
	}

	one := generateDiscNode()
	m[one.PublicKey()] = initial.Add(-minTimeBetweenQueries)

	expire(m)

	require.Equal(t, len(m), 99)

	_, ok := m[one.PublicKey()]
	require.False(t, ok)
}

func Test_pingThenFindNode(t *testing.T) {
	n := generateDiscNode()

	pingErr := errors.New("ping")
	findnodeErr := errors.New("findnode")

	p := &mockDisc{pingErr, nil, findnodeErr}

	c := make(chan queryResult, 1)
	pingThenGetAddresses(context.TODO(), p, n, c)

	require.Equal(t, len(c), 1)
	res := <-c
	require.Equal(t, res.err, pingErr)

	p.pingres = nil
	pingThenGetAddresses(context.TODO(), p, n, c)
	require.Equal(t, len(c), 1)
	res = <-c
	require.Equal(t, res.err, findnodeErr)

	p.findnoderr = nil
	p.findnoderes = []*node.Info{generateDiscNode()}

	pingThenGetAddresses(context.TODO(), p, n, c)

	require.Equal(t, len(c), 1)
	res = <-c
	require.Equal(t, res.err, nil)
	require.Equal(t, res.res, p.findnoderes)
}

func TestRefresher_refresh(t *testing.T) {
	cfg := config.DefaultConfig()
	local := generateDiscNode()
	disc := &mockDisc{}
	addrbk := newAddrBook(cfg.SwarmConfig, "", GetTestLogger("test.newRefresher.addrbook"))
	ref := newRefresher(local.PublicKey(), addrbk, disc, []*node.Info{}, GetTestLogger("test.newRefresher"))

	boot := generateDiscNode()

	addrbk.AddAddresses([]*node.Info{boot}, local)

	some := generateDiscNodes(10)
	disc.pingres = nil
	disc.findnoderr = nil
	disc.findnoderes = some

	res := ref.requestAddresses(context.TODO(), []*node.Info{boot})

	require.Equal(t, some, res)

	for _, s := range some {
		d, err := addrbk.Lookup(s.PublicKey())
		require.NoError(t, err)
		require.Equal(t, d.PublicKey(), s.PublicKey())
	}
}

func TestRefresher_refresh2(t *testing.T) {
	cfg := config.DefaultConfig()
	local := generateDiscNode()
	disc := &mockDisc{}
	addrbk := newAddrBook(cfg.SwarmConfig, "", GetTestLogger("test.newRefresher.addrbook"))
	ref := newRefresher(local.PublicKey(), addrbk, disc, []*node.Info{}, GetTestLogger("test.newRefresher"))

	boot := generateDiscNodes(3)

	addrbk.AddAddresses(boot, local)

	some := generateDiscNodes(10)
	disc.pingres = errors.New("ping")
	disc.findnoderr = nil
	disc.findnoderes = some

	res := ref.requestAddresses(context.TODO(), boot)

	require.Equal(t, len(res), 0)

	for _, s := range some {
		d, err := addrbk.Lookup(s.PublicKey())
		require.Error(t, err)
		require.Nil(t, d)
	}
}

func TestRefresher_refresh3(t *testing.T) {
	//test no duplicates
	cfg := config.DefaultConfig()
	local := generateDiscNode()
	disc := &mockDisc{}
	addrbk := newAddrBook(cfg.SwarmConfig, "", GetTestLogger("test.newRefresher.addrbook"))
	ref := newRefresher(local.PublicKey(), addrbk, disc, []*node.Info{}, GetTestLogger("test.newRefresher"))

	boot := generateDiscNodes(3)

	addrbk.AddAddresses(boot, local)

	some := generateDiscNodes(10)
	disc.pingres = nil
	disc.findnoderr = nil
	disc.findnoderes = some

	res := ref.requestAddresses(context.TODO(), boot)

	require.Equal(t, res, some)

	for _, s := range some {
		d, err := addrbk.Lookup(s.PublicKey())
		require.NoError(t, err)
		require.Equal(t, d, s)
	}
}

func TestRefresher_Bootstrap(t *testing.T) {
	cfg := config.DefaultConfig()
	local := generateDiscNode()
	disc := &mockDisc{}

	boot := generateDiscNodes(3)

	//for _, b := range boot {
	//	cfg.SwarmConfig.BootstrapNodes = append(cfg.SwarmConfig.BootstrapNodes, b.String())
	//}

	addrbk := newAddrBook(cfg.SwarmConfig, "", GetTestLogger("test.newRefresher.addrbook"))
	ref := newRefresher(local.PublicKey(), addrbk, disc, boot, GetTestLogger("test.newRefresher"))

	disc.pingres = nil
	disc.findnoderr = nil
	disc.findnoderes = generateDiscNodes(10)

	err := ref.Bootstrap(context.TODO(), 10)

	require.NoError(t, err)
}

func TestRefresher_BootstrapAbort(t *testing.T) {
	cfg := config.DefaultConfig()
	local := generateDiscNode()
	disc := &mockDisc{}

	boot := generateDiscNodes(3)

	//for _, b := range boot {
	//	cfg.SwarmConfig.BootstrapNodes = append(cfg.SwarmConfig.BootstrapNodes, b.String())
	//}

	addrbk := newAddrBook(cfg.SwarmConfig, "", GetTestLogger("test.newRefresher.addrbook"))
	ref := newRefresher(local.PublicKey(), addrbk, disc, boot, GetTestLogger("test.newRefresher"))

	disc.pingres = nil
	disc.findnoderr = nil
	disc.findnoderes = generateDiscNodes(2)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		err := ref.Bootstrap(ctx, 10)
		require.Equal(t, err, ErrBootAbort)
		done <- struct{}{}
	}()

	cancel()
	<-done
}

func TestRefresher_BootstrapTries(t *testing.T) {
	//cfg := config.DefaultConfig()
	local := generateDiscNode()
	disc := &mockDisc{}

	boot := generateDiscNodes(3)

	// Test refresher keeps going when addressbook is empty

	mckAddrbk := mockAddrBook{}
	counter := 0
	mckAddrbk.AddressCacheFunc = func() []*node.Info {
		counter++
		return nil
	}

	ref := newRefresher(local.PublicKey(), &mckAddrbk, disc, boot, GetTestLogger("test.newRefresher"))
	ref.backoffFunc = func(tries int) time.Duration {
		return 80 * time.Millisecond
	}
	ctx, c := context.WithTimeout(context.Background(), time.Second*1)
	err := ref.Bootstrap(ctx, 10)
	require.EqualError(t, err, ErrBootAbort.Error())
	require.True(t, counter > maxTries) // we got more than maxtries because we have no preloaded peers
	c()

	// Test refresher keeps going when we loaded less then required

	counter = 0
	mckAddrbk.NumAddressesFunc = func() int {
		return 1
	}
	mckAddrbk.AddressCacheFunc = func() []*node.Info {
		counter++
		return generateDiscNodes(1)
	}

	ctx, c1 := context.WithTimeout(context.Background(), time.Second*1)
	err = ref.Bootstrap(ctx, 10)
	require.EqualError(t, err, ErrBootAbort.Error())
	require.True(t, counter > maxTries)
	c1()

	ref.backoffFunc = func(tries int) time.Duration {
		return 0
	}
	// Test refresher stops after maxTries tries when address book has exactly or more than required peers
	counter = 0
	mckAddrbk.NumAddressesFunc = func() int {
		return 10
	}
	mckAddrbk.AddressCacheFunc = func() []*node.Info {
		counter++
		return generateDiscNodes(10)
	}

	ctx, c2 := context.WithTimeout(context.Background(), time.Second*1)
	err = ref.Bootstrap(ctx, 10)
	require.NoError(t, err)
	require.True(t, counter == maxTries) // we got more than maxtries because we have no preloaded peers
	c2()

}
