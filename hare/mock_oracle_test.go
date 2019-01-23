package hare

import (
	"encoding/binary"
	"github.com/stretchr/testify/assert"
	"math"
	"math/rand"
	"testing"
	"time"
)

const numOfClients = 100

func TestMockHashOracle_Register(t *testing.T) {
	oracle := NewMockHashOracle(numOfClients)
	oracle.Register(generateVerifier(t))
	oracle.Register(generateVerifier(t))
	assert.Equal(t, 2, len(oracle.clients))
}

func TestMockHashOracle_Unregister(t *testing.T) {
	oracle := NewMockHashOracle(numOfClients)
	pub := generateVerifier(t)
	oracle.Register(pub)
	assert.Equal(t, 1, len(oracle.clients))
	oracle.Unregister(pub)
	assert.Equal(t, 0, len(oracle.clients))
}

func TestMockHashOracle_Concurrency(t *testing.T) {
	oracle := NewMockHashOracle(numOfClients)
	c := make(chan Verifier, 1000)
	done := make(chan int, 2)

	go func() {
		for i := 0; i < 500; i++ {
			pub := generateVerifier(t)
			oracle.Register(pub)
			c <- pub
		}
		done <- 1
	}()

	go func() {
		for i := 0; i < 400; i++ {
			s := <-c
			oracle.Unregister(s)
		}
		done <- 1
	}()

	<-done
	<-done
	assert.Equal(t, len(oracle.clients), 100)
}

func genSig() Signature {
	s1 := rand.NewSource(time.Now().UnixNano())
	r1 := rand.New(s1)
	sig := make([]byte, 4, 4)
	binary.LittleEndian.PutUint32(sig, r1.Uint32())

	return sig[:]
}

func TestMockHashOracle_Role(t *testing.T) {
	oracle := NewMockHashOracle(numOfClients)
	for i := 0; i < numOfClients; i++ {
		pub := generateVerifier(t)
		oracle.Register(pub)
	}

	committeeSize := 20
	counter := 0
	for i := 0; i < numOfClients; i++ {
		if oracle.Validate(committeeSize, genSig()) {
			counter++
		}
	}

	if counter * 3 < committeeSize { // allow only deviation
		t.Errorf("Comity size error. Expected: %v Actual: %v", committeeSize, counter)
		t.Fail()
	}
}

func TestMockHashOracle_calcThreshold(t *testing.T) {
	oracle := NewMockHashOracle(2)
	oracle.Register(generateVerifier(t))
	oracle.Register(generateVerifier(t))
	assert.Equal(t, uint32(math.MaxUint32/2), oracle.calcThreshold(1))
	assert.Equal(t, uint32(math.MaxUint32), oracle.calcThreshold(2))
}
