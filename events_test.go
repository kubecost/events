package events

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type TestEvent struct {
	Message string
}

func waitAndDispatch[T any](dur time.Duration, dispatcher Dispatcher[T], event T) {
	time.Sleep(dur)
	dispatcher.Dispatch(event)
}

func waitChannelFor(wg *sync.WaitGroup) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		ch <- struct{}{}
	}()
	return ch
}

func strcmp[T comparable](t *testing.T, a, b T) {
	if a != b {
		t.Errorf("Expected %+v. Got: %+v", a, b)
	}
}

func TestTypeOf(t *testing.T) {
	strcmp(t, typeOf[TestEvent](), "github.com/kubecost/events/TestEvent")
	strcmp(t, typeOf[*TestEvent](), "*github.com/kubecost/events/TestEvent")
	strcmp(t, typeOf[**TestEvent](), "**github.com/kubecost/events/TestEvent")
}

func TestDispatchEventStream(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)

	d := NewDispatcher[TestEvent]()

	go waitAndDispatch(time.Second*1, d, TestEvent{"Test1"})
	go waitAndDispatch(time.Second*3, d, TestEvent{"Test2"})

	go func() {
		stream := d.NewEventStream()

		for event := range stream.Stream() {
			t.Logf("Event: %s\n", event.Message)
			wg.Done()
		}
	}()

	select {
	case <-time.After(5 * time.Second):
		t.Errorf("Test failed. Timed out after 5 seconds\n")
	case <-waitChannelFor(&wg):
		t.Logf("Completed successfully\n")
	}
}

func TestSingleDispatcherMultipleHandlers(t *testing.T) {
	const totalEvents = 10

	var wg sync.WaitGroup

	// for 10 events and 3 handlers, we'll expect to receive
	// 30 total handler calls (10 events per handler)
	wg.Add(3 * totalEvents)

	d := NewDispatcher[TestEvent]()

	var handlerOneCount uint64 = 0
	d.AddEventHandler(func(event TestEvent) {
		atomic.AddUint64(&handlerOneCount, 1)
		t.Logf("Handler One: [Event: %s] - Total: %d\n", event.Message, handlerOneCount)
		wg.Done()
	})

	var handlerTwoCount uint64 = 0
	d.AddEventHandler(func(event TestEvent) {
		atomic.AddUint64(&handlerTwoCount, 1)
		t.Logf("Handler Two: [Event: %s] - Total: %d\n", event.Message, handlerTwoCount)
		wg.Done()
	})

	var handlerThreeCount uint64 = 0
	d.AddEventHandler(func(event TestEvent) {
		atomic.AddUint64(&handlerThreeCount, 1)
		t.Logf("Handler Three: [Event: %s] - Total: %d\n", event.Message, handlerThreeCount)
		wg.Done()
	})

	for i := 0; i < totalEvents; i++ {
		d.Dispatch(TestEvent{fmt.Sprintf("%d", i+1)})
	}

	select {
	case <-time.After(5 * time.Second):
		t.Errorf("Test failed. Timed out after 5 seconds\n")
	case <-waitChannelFor(&wg):
		t.Logf("Completed successfully\n")
	}
}

func TestAddRemoveHandlersMidStream(t *testing.T) {
	const totalEvents = 10

	d := NewDispatcher[TestEvent]()

	var eventCount uint64 = 0
	var h1 HandlerID
	h1 = d.AddEventHandler(func(event TestEvent) {
		atomic.AddUint64(&eventCount, 1)
		t.Logf("Handler One: [Event: %s]\n", event.Message)
		d.RemoveEventHandler(h1)
	})

	var h2 HandlerID
	h2 = d.AddEventHandler(func(event TestEvent) {
		atomic.AddUint64(&eventCount, 1)
		t.Logf("Handler Two: [Event: %s]\n", event.Message)
		d.RemoveEventHandler(h2)
	})

	var h3 HandlerID
	h3 = d.AddEventHandler(func(event TestEvent) {
		atomic.AddUint64(&eventCount, 1)
		t.Logf("Handler Three: [Event: %s]\n", event.Message)
		d.RemoveEventHandler(h3)
	})

	for i := 0; i < totalEvents; i++ {
		d.Dispatch(TestEvent{fmt.Sprintf("%d", i+1)})
	}

	time.Sleep(2 * time.Second)
	if eventCount != 3 {
		t.Errorf("Event Count != 3. Got: %d\n", eventCount)
	}

	// test that the internal event stream list is empty
	eventStreams := len(d.(*multicastDispatcher[TestEvent]).getEventStreams())
	if eventStreams != 0 {
		t.Errorf("Event Streams were not empty. Got: %d\n", eventStreams)
	}
}

func TestAddRemoveHandlersMidStreamSync(t *testing.T) {
	const totalEvents = 10

	d := NewDispatcher[TestEvent]()

	// Synchronous Events require a receiver, so we create a single always running receiver
	s := d.NewEventStream()
	go func() {
		for event := range s.Stream() {
			t.Logf("Universal Handler: [Event %s]\n", event.Message)
		}
	}()

	var eventCount uint64 = 0
	var h1 HandlerID
	h1 = d.AddEventHandler(func(event TestEvent) {
		atomic.AddUint64(&eventCount, 1)
		t.Logf("Handler One: [Event: %s]\n", event.Message)
		d.RemoveEventHandler(h1)
	})

	var h2 HandlerID
	h2 = d.AddEventHandler(func(event TestEvent) {
		atomic.AddUint64(&eventCount, 1)
		t.Logf("Handler Two: [Event: %s]\n", event.Message)
		d.RemoveEventHandler(h2)
	})

	var h3 HandlerID
	h3 = d.AddEventHandler(func(event TestEvent) {
		atomic.AddUint64(&eventCount, 1)
		t.Logf("Handler Three: [Event: %s]\n", event.Message)
		d.RemoveEventHandler(h3)
	})

	for i := 0; i < totalEvents; i++ {
		d.DispatchSync(TestEvent{fmt.Sprintf("%d", i+1)})
	}

	time.Sleep(2 * time.Second)
	if eventCount != 3 {
		t.Errorf("Event Count != 3. Got: %d\n", eventCount)
	}

	d.CloseEventStreams()

	// test that the internal event stream list is empty
	eventStreams := len(d.(*multicastDispatcher[TestEvent]).getEventStreams())
	if eventStreams != 0 {
		t.Errorf("Event Streams were not empty. Got: %d\n", eventStreams)
	}
}

/*
func TestStoredChan(t *testing.T) {
	ch := make(chan int)

	go func() {
		ch <- 1
	}()
	go func() {
		ch <- 2
	}()

	// wait a second to ensure both go routines have run and are pushing
	// the int to the channel
	time.Sleep(time.Second)

	// Only pull one int from the channel
	i := <-ch
	t.Logf("I: %d\n", i)

	// close the channel with the other being pushed on the channel
	close(ch)

	// wait, and it will panic
	time.Sleep(2 * time.Second)
}
*/
