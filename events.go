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
	t := reflect.TypeOf(*new(T))
	return fmt.Sprintf("%s/%s", t.PkgPath(), t.Name())
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
