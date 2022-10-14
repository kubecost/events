package events

import (
	"fmt"
	"reflect"
	"sync"
	"time"
)

var (
	lock         sync.Mutex
	dispatchers  map[string]any
	dispatchers2 map[reflect.Type]any
)

func init() {
	dispatchers = make(map[string]any)
}

// typeOf is a utility that can covert a T type to a package + type name for generic types.
func typeOf[T any]() string {
	var inst T
	var prefix string

	// get a reflect.Type of a variable with type T
	t := reflect.TypeOf(inst)

	// pointer types do not carry the adequate type information, so we need to extract the
	// underlying types until we reach the non-pointer type, we prepend a * each depth
	for t != nil && t.Kind() == reflect.Pointer {
		prefix += "*"
		t = t.Elem()
	}

	// this should not be possible, but in the event that it does, we want to be loud about
	// it
	if t == nil {
		panic(fmt.Sprintf("Unable to generate a key for type: %+v", reflect.TypeOf(inst)))
	}

	// combine the prefix, package path, and the type name
	return fmt.Sprintf("%s%s/%s", prefix, t.PkgPath(), t.Name())
}

// GlobalDispatcherFor[T] locates an existing global dispatcher for an event type, or
// creates a new one if one does not exist
func GlobalDispatcherFor[T any]() Dispatcher[T] {
	lock.Lock()
	defer lock.Unlock()

	key := typeOf[T]()
	if _, ok := dispatchers[key]; !ok {
		dispatchers[key] = newMulticastDispatcher[T](true)
	}

	return dispatchers[key].(Dispatcher[T])
}

// Dispatch is a convenience method which dispatches an event using the global dispatcher
// for a specific T event type.
func Dispatch[T any](event T) {
	GlobalDispatcherFor[T]().Dispatch(event)
}

// DispatchSync is a convenience method which synchronously dispatches an event using the
// global dispatcher for a specific T event type.
func DispatchSync[T any](event T) {
	GlobalDispatcherFor[T]().DispatchSync(event)
}

// Dispatch is a convenience method which synchronously dispatches an event with a timeout
// using the global dispatcher for a specific T event type.
func DispatchSyncWithTimeout[T any](event T, timeout time.Duration) {
	GlobalDispatcherFor[T]().DispatchSyncWithTimeout(event, timeout)
}

// NewDispatcher[T] creates a new multicast event dispatcher
func NewDispatcher[T any]() Dispatcher[T] {
	return newMulticastDispatcher[T](false)
}
