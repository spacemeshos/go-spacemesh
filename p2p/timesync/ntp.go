package timesync

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"math/rand"
	"net"
	"time"
)

//TODO: config params for NTP_QUERIES, DEFAULT_TIMEOUT_LATENCY, NTP_QUERIES, REFRESH_NTP_INTERVAL
const (
	// 70 years in seconds since ntp counts from 1900 and unix from 1970
	NTP_OFFSET              = 2208988800
	DEFAULT_NTP_PORT        = "123"
	MAX_ALLOWED_DRIFT       = 10 * time.Second
	NTP_QUERIES             = 4
	DEFAULT_TIMEOUT_LATENCY = 30 * time.Second
	REFRESH_NTP_INTERVAL    = 30 * time.Minute
)

// Relay on more than one server
var (
	DEFAULT_SERVERS = []string{
		"0.pool.ntp.org",
		"1.pool.ntp.org",
		"time1.google.com",
		"time2.google.com",
		"time.asia.apple.com",
		"time.americas.apple.com",
	}
	zeroDuration = time.Duration(0)
	zeroTime     = time.Time{}
)

// 48 bytes packet.
type NtpPacket struct {
	Settings       uint8  // leap yr indicator, ver number, and mode
	Stratum        uint8  // stratum of local clock
	Poll           int8   // poll exponent
	Precision      int8   // precision exponent
	RootDelay      uint32 // root delay
	RootDispersion uint32 // root dispersion
	ReferenceID    uint32 // reference id
	RefTimeSec     uint32 // reference timestamp sec
	RefTimeFrac    uint32 // reference timestamp fractional
	OrigTimeSec    uint32 // origin time secs
	OrigTimeFrac   uint32 // origin time fractional
	RxTimeSec      uint32 // receive time secs
	RxTimeFrac     uint32 // receive time frac3
	TxTimeSec      uint32 // transmit time secs
	TxTimeFrac     uint32 // transmit time frac

}

// Time makes a Time struct from NtpPacket data
func (n *NtpPacket) Time() time.Time {
	secs := float64(n.TxTimeSec) - NTP_OFFSET
	nanos := (int64(n.TxTimeFrac) * 1e9) >> 32
	return time.Unix(int64(secs), nanos)
}

// ntpRequest requests a Ntp packet from a server and  request time, latency and a NtpPacket struct
func ntpRequest(server string, rq *NtpPacket) (time.Time, time.Duration, *NtpPacket, error) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(server, DEFAULT_NTP_PORT))
	if err != nil {
		return zeroTime, zeroDuration, nil, err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Error("failed to connect:", err)
		return zeroTime, zeroDuration, nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(
		time.Now().Add(DEFAULT_TIMEOUT_LATENCY)); err != nil {
		log.Error("failed to set deadline: ", err)
		return zeroTime, zeroDuration, nil, err
	}
	before := time.Now()
	if err := binary.Write(conn, binary.BigEndian, rq); err != nil {
		log.Error("failed to send request: %v", err)
		return zeroTime, zeroDuration, nil, err
	}
	latency := time.Since(before)
	rsp := &NtpPacket{}
	if err := binary.Read(conn, binary.BigEndian, rsp); err != nil {
		log.Error("failed to read server response: %v", err)
		return zeroTime, zeroDuration, nil, err
	}

	return before, latency, rsp, nil
}

// ntpTimeDrift queries random servers from our list to calculate a drift average
func ntpTimeDrift() (time.Duration, error) {

	// 00 011 011 = 0x1B
	// Leap = 0
	// Client mode = 3
	// Version = 3
	resultsChan := make(chan time.Duration)
	errorChan := make(chan error)
	req := &NtpPacket{Settings: 0x1B}

	// Make 3 concurrent calls to different ntp servers
	// TODO: possibly add retries when timeout
	queriedServers := make(map[int]bool)
	serverSeed := len(DEFAULT_SERVERS) - 1
	for i := 0; i < NTP_QUERIES; i++ {
		rndsrv := rand.Intn(serverSeed)
		for queriedServers[rndsrv] {
			rndsrv = rand.Intn(serverSeed)
		}
		queriedServers[rndsrv] = true
		go func() {
			rt, lat, rsp, err := ntpRequest(DEFAULT_SERVERS[rndsrv], req)
			if err != nil {
				errorChan <- err
				return
			}
			// Calculate drift with latency
			drift := rt.UTC().Sub(rsp.Time().UTC().Add(lat / 2))
			resultsChan <- drift
		}()
	}

	all := time.Duration(0)
	for i := 0; i < NTP_QUERIES; i++ {
		select {
		case err := <-errorChan:
			close(errorChan)
			return all, err
		case result := <-resultsChan:
			all += result
		}
	}
	// return an average of all collected drifts
	// TODO: ignore extreme edge results
	return time.Duration(all / NTP_QUERIES), nil
}

// CheckSystemClockDrift is comparing our clock to the collected ntp data
// return the drift and an error when drift reading failed or exceeds our preset MAX_ALLOWED_DRIFT
func CheckSystemClockDrift() (time.Duration, error) {
	// Read average drift form ntpTimeDrift
	drift, err := ntpTimeDrift()
	if err != nil {
		return drift, err
	}
	// Check if drift exceeds our max allowed drift
	if drift < -MAX_ALLOWED_DRIFT || drift > MAX_ALLOWED_DRIFT {
		return drift, errors.New(fmt.Sprintf("System clock is %s away from NTP servers. please synchronize your OS", drift))
	}

	return drift, nil
}
