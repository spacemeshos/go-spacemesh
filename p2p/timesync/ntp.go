package timesync

import (
	"encoding/binary"
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"net"
	"time"
)

const (
	// 70 years in seconds since ntp counts from 1900 and unix from 1970
	NTP_OFFSET              = 2208988800
	DEFAULT_NTP_PORT        = "123"
	MAX_ALLOWED_DRIFT       = 10 * time.Second
	NTP_QUERIES             = 4
	DEFAULT_TIMEOUT_LATENCY = 30 * time.Second
)

// Relay on more than one server
var DEFAULT_SERVERS = []string{
	"0.asia.pool.ntp.org",
	"1.asia.pool.ntp.org",
	"2.asia.pool.ntp.org",
	"3.asia.pool.ntp.org",
}

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

func (n *NtpPacket) Time() time.Time {
	secs := float64(n.TxTimeSec) - NTP_OFFSET
	nanos := (int64(n.TxTimeFrac) * 1e9) >> 32
	return time.Unix(int64(secs), nanos)
}

// ntpRequest requests a Ntp packet from a server and returns a NtpPacket struct
func ntpRequest(server string, rq *NtpPacket) (*NtpPacket, error) {

	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(server, DEFAULT_NTP_PORT))
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Error("failed to connect:", err)
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(
		time.Now().Add(DEFAULT_TIMEOUT_LATENCY)); err != nil {
		log.Error("failed to set deadline: ", err)
		return nil, err
	}

	if err := binary.Write(conn, binary.BigEndian, rq); err != nil {
		log.Error("failed to send request: %v", err)
		return nil, err
	}

	rsp := &NtpPacket{}
	if err := binary.Read(conn, binary.BigEndian, rsp); err != nil {
		log.Error("failed to read server response: %v", err)
		return nil, err
	}

	return rsp, nil
}

func ntpTimeDrift() (time.Duration, error) {

	// 00 011 011 = 0x1B
	// Leap = 0
	// Client mode = 3
	// Version = 3
	resultsChan := make(chan time.Duration)
	errorChan := make(chan error)
	results := []time.Duration{}
	req := &NtpPacket{Settings: 0x1B}

	// Make 3 concurrent calls to different ntp servers and calculate an average drift
	serverIndex := 0
	for i := 0; i < NTP_QUERIES; i++ {
		go func() {
			if serverIndex >= len(DEFAULT_SERVERS) || serverIndex < 0 {
				serverIndex = 0
			}
			server := DEFAULT_SERVERS[serverIndex]
			serverIndex++

			before := time.Now()
			rsp, err := ntpRequest(server, req)
			if err != nil {
				log.Error("Failed to query NTP ", err)
				errorChan <- err
			}
			latency := time.Since(before)
			drift := before.UTC().Sub(rsp.Time().UTC().Add(latency / 2))
			resultsChan <- drift
		}()
	}

	all := time.Duration(0)
	select {
	case err := <-errorChan:
		close(errorChan)
		return all, err
	case result := <-resultsChan:
		all += result
		if len(results) == NTP_QUERIES {
			close(resultsChan)
		}
	}

	return time.Duration(int(all) / NTP_QUERIES), nil
}

func CheckSystemClockDrift() error {
	drift, err := ntpTimeDrift()
	if err != nil {
		return err
	}

	if drift < -MAX_ALLOWED_DRIFT || drift > MAX_ALLOWED_DRIFT {
		return errors.New("System clock is too far from NTP servers. please synchronize your OS")
	}

	log.Info("System clock is accurate with ntp allowed drift: %v", drift)
	return nil
}
