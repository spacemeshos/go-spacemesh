// Package timesync is used to check system time reliability by communicating with NTP time servers.
package timesync

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"time"
)

//TODO: config params for NtpQueries, DefaultTimeoutLatency, NtpQueries, refreshNtpInterval
const (
	// 70 years in seconds since ntp counts from 1900 and unix from 1970
	NtpOffset             = 2208988800
	DefaultNtpPort        = "123"
	MaxAllowedDrift       = 10 * time.Second
	NtpQueries            = 4
	DefaultTimeoutLatency = 30 * time.Second
	RefreshNtpInterval    = 30 * time.Minute
)

// DefaultServer is a list of relay on more than one server.
var (
	DefaultServers = []string{
		"0.pool.ntp.org",
		"1.pool.ntp.org",
		"time.google.com",
		"time1.google.com",
		"time.asia.apple.com",
		"time.americas.apple.com",
	}
	zeroDuration = time.Duration(0)
	zeroTime     = time.Time{}
)

// NtpPacket is a 48 bytes packet used for querying ntp information.
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

// Time makes a Time struct from NtpPacket data.
func (n *NtpPacket) Time() time.Time {
	secs := float64(n.TxTimeSec) - NtpOffset
	nanos := (int64(n.TxTimeFrac) * 1e9) >> 32
	return time.Unix(int64(secs), nanos)
}

// ntpRequest requests a Ntp packet from a server and  request time, latency and a NtpPacket struct.
func ntpRequest(server string, rq *NtpPacket) (time.Time, time.Duration, *NtpPacket, error) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(server, DefaultNtpPort))
	if err != nil {
		return zeroTime, zeroDuration, nil, err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return zeroTime, zeroDuration, nil, fmt.Errorf("failed to connect: %v", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(
		time.Now().Add(DefaultTimeoutLatency)); err != nil {
		return zeroTime, zeroDuration, nil, fmt.Errorf("failed to set deadline: %s", err)
	}
	before := time.Now()
	if err := binary.Write(conn, binary.BigEndian, rq); err != nil {
		return zeroTime, zeroDuration, nil, fmt.Errorf("failed to send request: %v", err)
	}
	latency := time.Since(before)
	rsp := &NtpPacket{}
	if err := binary.Read(conn, binary.BigEndian, rsp); err != nil {
		return zeroTime, zeroDuration, nil, fmt.Errorf("failed to read server response: %v", err)
	}

	return before, latency, rsp, nil
}

// ntpTimeDrift queries random servers from our list to calculate a drift average.
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
	serverSeed := len(DefaultServers) - 1
	for i := 0; i < NtpQueries; i++ {
		rndsrv := rand.Intn(serverSeed)
		for queriedServers[rndsrv] {
			rndsrv = rand.Intn(serverSeed)
		}
		queriedServers[rndsrv] = true
		go func() {
			rt, lat, rsp, err := ntpRequest(DefaultServers[rndsrv], req)
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
	for i := 0; i < NtpQueries; i++ {
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
	return time.Duration(all / NtpQueries), nil
}

// CheckSystemClockDrift is comparing our clock to the collected ntp data
// return the drift and an error when drift reading failed or exceeds our preset MaxAllowedDrift
func CheckSystemClockDrift() (time.Duration, error) {
	// Read average drift form ntpTimeDrift
	drift, err := ntpTimeDrift()
	if err != nil {
		return drift, err
	}
	// Check if drift exceeds our max allowed drift
	if drift < -MaxAllowedDrift || drift > MaxAllowedDrift {
		return drift, fmt.Errorf("System clock is %s away from NTP servers. please synchronize your OS ", drift)
	}

	return drift, nil
}
