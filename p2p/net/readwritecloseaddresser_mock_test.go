package net

import (
	"net"
	"time"
)

// ReadWriteCloseAddresserMock is a ninja robot
type ReadWriteCloseAddresserMock struct {
	readIn   []byte
	readErr  error
	readCnt  int
	readChan chan struct{}

	writeWaitChan chan []byte

	writeErr error
	writeOut []byte
	writeCnt int

	closeRes error
	closeCnt int

	remoteAddrRes net.Addr
	remoteAddrCnt int
}

// NewReadWriteCloseAddresserMock is this
func NewReadWriteCloseAddresserMock() *ReadWriteCloseAddresserMock {
	return &ReadWriteCloseAddresserMock{
		readChan: make(chan struct{}, 1),
	}
}

// SetReadResult is this
func (rwcam *ReadWriteCloseAddresserMock) SetReadResult(p []byte, err error) {
	rwcam.readIn = make([]byte, len(p))
	copy(rwcam.readIn, p)
	rwcam.readErr = err
	rwcam.readChan <- struct{}{}
}

// ReadCount is this
func (rwcam *ReadWriteCloseAddresserMock) ReadCount() int {
	return rwcam.readCnt
}

func (rwcam *ReadWriteCloseAddresserMock) SetReadDeadline(t time.Time) error {
	return nil
}

func (rwcam *ReadWriteCloseAddresserMock) SetWriteDeadline(t time.Time) error {
	return nil
}

// Read is this
func (rwcam *ReadWriteCloseAddresserMock) Read(p []byte) (n int, err error) {
	rwcam.readCnt++
	<-rwcam.readChan
	err = rwcam.readErr
	n = 0
	if rwcam.readErr == nil {
		n = copy(p, rwcam.readIn)
	}
	return
}

// SetWriteResult is a mock
func (rwcam *ReadWriteCloseAddresserMock) SetWriteResult(err error) {
	rwcam.writeErr = err
}

// WriteOut is a mock
func (rwcam *ReadWriteCloseAddresserMock) WriteOut() (p []byte) {
	p = append(p, rwcam.writeOut...)
	return
}

// WriteCount is a mock
func (rwcam *ReadWriteCloseAddresserMock) WriteCount() int {
	return rwcam.writeCnt
}

// Write is a mock
func (rwcam *ReadWriteCloseAddresserMock) Write(p []byte) (n int, err error) {
	rwcam.writeCnt++
	n = 0
	err = rwcam.writeErr
	if rwcam.writeErr == nil {
		rwcam.writeOut = append(rwcam.writeOut, p...)
	}
	if rwcam.writeWaitChan != nil {
		rwcam.writeWaitChan <- p
	}
	return
}

// CloseCount oh yeah
func (rwcam *ReadWriteCloseAddresserMock) CloseCount() int {
	return rwcam.closeCnt
}

// Close is mock close
func (rwcam *ReadWriteCloseAddresserMock) Close() error {
	rwcam.closeCnt++
	close(rwcam.readChan)
	return rwcam.closeRes
}

func (rwcam *ReadWriteCloseAddresserMock) setRemoteAddrResult(addr net.Addr) {
	rwcam.remoteAddrRes = addr
}

// RemoteAddr is a RemoteAddr mock
func (rwcam *ReadWriteCloseAddresserMock) RemoteAddr() net.Addr {
	rwcam.remoteAddrCnt++
	return rwcam.remoteAddrRes
}
