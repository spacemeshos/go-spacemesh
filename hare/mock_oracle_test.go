package hare

import (
	"encoding/binary"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/stretchr/testify/assert"
	"math"
	"math/rand"
	"testing"
	"time"
)

const comitySize = 18
const numOfClients = 20

func TestMockHashOracle_Register(t *testing.T) {
	oracle := NewMockHashOracle(numOfClients, comitySize)
	oracle.Register(generatePubKey(t))
	oracle.Register(generatePubKey(t))
	assert.Equal(t, 2, len(oracle.clients))
}

func TestMockHashOracle_Unregister(t *testing.T) {
	oracle := NewMockHashOracle(numOfClients, comitySize)
	pub := generatePubKey(t)
	oracle.Register(pub)
	assert.Equal(t, 1, len(oracle.clients))
	oracle.Unregister(pub)
	assert.Equal(t, 0, len(oracle.clients))
}

func TestMockHashOracle_Concurrency(t *testing.T) {
	oracle := NewMockHashOracle(numOfClients, comitySize)
	c := make(chan crypto.PublicKey, 1000)
	done := make(chan int, 2)

	go func() {
		for i := 0; i < 500; i++ {
			pub := generatePubKey(t)
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
	oracle := NewMockHashOracle(numOfClients, comitySize)
	for i := 0; i < numOfClients; i++ {
		pub := generatePubKey(t)
		oracle.Register(pub)
	}

	passive := 0
	active := 0
	leader := 0
	for i := 0; i < numOfClients; i++ {
		r := oracle.Role(genSig())
		switch r {
		case Passive:
			passive++
		case Active:
			active++
		case Leader:
			leader++
		default:
			t.Fail()
		}
	}

	//fmt.Printf("P=%v A=%v L=%v\n", passive, active, leader)

	total := active + leader
	if math.Abs(float64(comitySize - total)) > 10 * comitySize { // allow only 10% deviation
		t.Errorf("Comity size error. Expected: %v Actual %v", comitySize, total)
		t.Fail()
	}

	if leader > 10 {
		t.Errorf("Too many leaders. Expected: %v Actual %v", 1, leader)
		t.Fail()
	}
}

func TestMockHashOracle_calcThreshold(t *testing.T) {
	oracle := NewMockHashOracle(2, 2)
	oracle.Register(generatePubKey(t))
	oracle.Register(generatePubKey(t))
	assert.Equal(t, uint32(math.MaxUint32 / 2), oracle.calcThreshold(1))
	assert.Equal(t, uint32(math.MaxUint32), oracle.calcThreshold(2))
}