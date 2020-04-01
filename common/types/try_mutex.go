package types

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

const mutexUnLocked = 0
const mutexLocked = 1

// TryMutex is a simple sync.Mutex with the ability to try to Lock.
type TryMutex struct {
	sync.Mutex
}

// TryLock tries to lock m. It returns true in case of success, false otherwise.
func (m *TryMutex) TryLock() bool {
	return atomic.CompareAndSwapInt32((*int32)(unsafe.Pointer(&m.Mutex)), mutexUnLocked, mutexLocked)
}
