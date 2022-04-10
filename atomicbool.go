package events

import (
	"sync/atomic"
)

// atomic boolean alias leverages a 32-bit integer CAS
type atomicbool int32

// newAtomicBool creates an atomicbool with given default value
func newAtomicBool(value bool) *atomicbool {
	ab := new(atomicbool)
	ab.Set(value)
	return ab
}

// Loads the bool value atomically
func (ab *atomicbool) Get() bool {
	return atomic.LoadInt32((*int32)(ab)) != 0
}

// Sets the bool value atomically
func (ab *atomicbool) Set(value bool) {
	if value {
		atomic.StoreInt32((*int32)(ab), 1)
	} else {
		atomic.StoreInt32((*int32)(ab), 0)
	}
}

// CompareAndSet sets value to new if current is equal to the current value. If the new value is
// set, this function returns true.
func (ab *atomicbool) CompareAndSet(current, new bool) bool {
	var o, n int32
	if current {
		o = 1
	}
	if new {
		n = 1
	}
	return atomic.CompareAndSwapInt32((*int32)(ab), o, n)
}
