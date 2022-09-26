package hare

import (
	"context"
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const numOfClients = 100

func TestMockHashOracle_Register(t *testing.T) {
	oracle := newMockHashOracle(numOfClients)
	oracle.Register(signing.NewEdSigner([20]byte{}).PublicKey().String())
	oracle.Register(signing.NewEdSigner([20]byte{}).PublicKey().String())
	assert.Equal(t, 2, len(oracle.clients))
}

func TestMockHashOracle_Unregister(t *testing.T) {
	oracle := newMockHashOracle(numOfClients)
	pub := signing.NewEdSigner([20]byte{})
	oracle.Register(pub.PublicKey().String())
	assert.Equal(t, 1, len(oracle.clients))
	oracle.Unregister(pub.PublicKey().String())
	assert.Equal(t, 0, len(oracle.clients))
}

func TestMockHashOracle_Concurrency(t *testing.T) {
	oracle := newMockHashOracle(numOfClients)
	c := make(chan Signer, 1000)
	done := make(chan int, 2)

	go func() {
		for i := 0; i < 500; i++ {
			pub := signing.NewEdSigner([20]byte{})
			oracle.Register(pub.PublicKey().String())
			c <- pub
		}
		done <- 1
	}()

	go func() {
		for i := 0; i < 400; i++ {
			s := <-c
			oracle.Unregister(s.PublicKey().String())
		}
		done <- 1
	}()

	<-done
	<-done
	assert.Equal(t, len(oracle.clients), 100)
}

func genSig() []byte {
	s1 := rand.NewSource(time.Now().UnixNano())
	r1 := rand.New(s1)
	sig := make([]byte, 4)
	binary.LittleEndian.PutUint32(sig, r1.Uint32())
	return sig[:]
}

func TestMockHashOracle_Role(t *testing.T) {
	oracle := newMockHashOracle(numOfClients)
	for i := 0; i < numOfClients; i++ {
		pub := signing.NewEdSigner([20]byte{})
		oracle.Register(pub.PublicKey().String())
	}

	committeeSize := 20
	counter := 0
	for i := 0; i < numOfClients; i++ {
		res, _ := oracle.eligible(context.TODO(), types.LayerID{}, 1, committeeSize, types.BytesToNodeID(signing.NewEdSigner([20]byte{}).PublicKey().Bytes()), []byte(genSig()))
		if res {
			counter++
		}
	}

	if counter*3 < committeeSize { // allow only deviation
		t.Errorf("Comity size error. Expected: %v Actual: %v", committeeSize, counter)
		t.Fail()
	}
}

func TestMockHashOracle_calcThreshold(t *testing.T) {
	oracle := newMockHashOracle(2)
	oracle.Register(signing.NewEdSigner([20]byte{}).PublicKey().String())
	oracle.Register(signing.NewEdSigner([20]byte{}).PublicKey().String())
	assert.Equal(t, uint32(math.MaxUint32/2), oracle.calcThreshold(1))
	assert.Equal(t, uint32(math.MaxUint32), oracle.calcThreshold(2))
}
