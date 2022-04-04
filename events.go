package events

import (
	"fmt"
	"reflect"
	"sync"
)

var (
	lock        sync.Mutex
	dispatchers map[string]interface{}
)

func init() {
	dispatchers = make(map[string]interface{})
}

// typeOf is a utility that can covert a T type to a package + type name for generic types.
func typeOf[T any]() string {
	var inst T
	var prefix string

	// get a reflect.Type of a variable with type T
	t := reflect.TypeOf(inst)

	// pointer types do not carry the adequate type information, so we need to extract the
	// underlying types until we reach the non-pointer type, we prepend a * each depth
	for t != nil && t.Kind() == reflect.Ptr {
		prefix += "*"
		t = t.Elem()
	}

	// this should not be possible, but in the event that it does, we want to be loud about it
	if t == nil {
		panic(fmt.Sprintf("Unable to generate a key for type: %+v", reflect.TypeOf(inst)))
	}

	// combine the prefix, package path, and the type name
	return fmt.Sprintf("%s%s/%s", prefix, t.PkgPath(), t.Name())
}

// DispatcherFor[T] locates an existing global dispatcher for an event type, or creates a new one
// if one does not exist
func DispatcherFor[T any]() Dispatcher[T] {
	lock.Lock()
	defer lock.Unlock()

	key := typeOf[T]()
	if _, ok := dispatchers[key]; !ok {
		dispatchers[key] = newMulticastDispatcher[T]()
	}

	return dispatchers[key].(Dispatcher[T])
}
