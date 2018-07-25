package net

import (
	"net"
	"sync"
)

type ReadWriteCloseAddresserMock struct {
	readIn  []byte
	readErr error
	readCnt int
	readChan chan struct{}

	writeErr error
	writeOut []byte
	writeCnt int
	lock     sync.Mutex

	closeRes error
	closeCnt int

	remoteAddrRes net.Addr
	remoteAddrCnt int
}

func NewReadWriteCloseAddresserMock() *ReadWriteCloseAddresserMock {
	return &ReadWriteCloseAddresserMock{
		readChan: make(chan struct{}),
	}
}

func (rwcam *ReadWriteCloseAddresserMock) SetReadResult(p []byte, err error) {
	rwcam.readIn = make([]byte, len(p))
	copy(rwcam.readIn, p)
	rwcam.readErr = err
	rwcam.readChan <- struct{}{}
}

func (rwcam *ReadWriteCloseAddresserMock) ReadCount() int {
	return rwcam.readCnt
}

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

func (rwcam *ReadWriteCloseAddresserMock) SetWriteResult(err error) {
	rwcam.writeErr = err
}

func (rwcam *ReadWriteCloseAddresserMock) WriteOut() (p []byte) {
	p = append(p, rwcam.writeOut...)
	return
}

func (rwcam *ReadWriteCloseAddresserMock) WriteCount() int {
	return rwcam.writeCnt
}

func (rwcam *ReadWriteCloseAddresserMock) Write(p []byte) (n int, err error) {
	rwcam.writeCnt++
	n = 0
	err = rwcam.writeErr
	if rwcam.writeErr == nil {
		rwcam.writeOut = append(rwcam.writeOut, p...)
	}
	return
}

func (rwcam *ReadWriteCloseAddresserMock) setCloseResult(err error) {
	rwcam.closeRes = err
}

func (rwcam *ReadWriteCloseAddresserMock) CloseCount() int {
	return rwcam.closeCnt
}

func (rwcam *ReadWriteCloseAddresserMock) Close() error {
	rwcam.closeCnt++
	close(rwcam.readChan)
	return rwcam.closeRes
}

func (rwcam *ReadWriteCloseAddresserMock) setRemoteAddrResult(addr net.Addr) {
	rwcam.remoteAddrRes = addr
}

func (rwcam *ReadWriteCloseAddresserMock) remoteAddrCount() int {
	return rwcam.remoteAddrCnt
}

func (rwcam *ReadWriteCloseAddresserMock) RemoteAddr() net.Addr {
	rwcam.remoteAddrCnt++
	return rwcam.remoteAddrRes
}