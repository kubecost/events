package events

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

//--------------------------------------------------------------------------
//  TypedEventKind Enum
//--------------------------------------------------------------------------

// TypedEventKind is used as a filterable field for events.
type TypedEventKind int

const (
	TypedEventKindOne TypedEventKind = iota
	TypedEventKindTwo
	TypedEventKindThree
)

//--------------------------------------------------------------------------
//  Testing Data Structures
//--------------------------------------------------------------------------

// TestEvent is a simple message wrapper.
type TestEvent struct {
	Message string
}

// GenericTestEvent is a message and data wrapper.
type GenericTestEvent[T any] struct {
	Message string
	Data    T
}

// TypedEvent is a message wrapper with a kind field used to test filtered dispatch.
type TypedEvent struct {
	Kind    TypedEventKind
	Message string
}

// waitAndDispatch sleeps for the specified duration, then dispatches the event using the
// provided dispatcher
func waitAndDispatch[T any](dur time.Duration, dispatcher Dispatcher[T], event T) {
	time.Sleep(dur)
	dispatcher.Dispatch(event)
}

// waitChannelFor creates returns a channel that will send a signal when the waitgroup is
// done.
func waitChannelFor(wg *sync.WaitGroup) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		ch <- struct{}{}
	}()
	return ch
}

// cmp compares two comparable values and fails the test if they are not equal.
func cmp[T comparable](t *testing.T, a, b T) {
	if a != b {
		t.Errorf("Expected %+v. Got: %+v", a, b)
	}
}

// eventFilter returns a closure that can be used to filter events based on their kind.
func eventFilter(kind TypedEventKind) EventCondition[TypedEvent] {
	return func(event TypedEvent) bool {
		return event.Kind == kind
	}
}

//--------------------------------------------------------------------------
//  Tests
//--------------------------------------------------------------------------

func TestTypeOf(t *testing.T) {
	cmp(t, typeOf[TestEvent](), "github.com/kubecost/events/TestEvent")
	cmp(t, typeOf[*TestEvent](), "*github.com/kubecost/events/TestEvent")
	cmp(t, typeOf[**TestEvent](), "**github.com/kubecost/events/TestEvent")
	cmp(t, typeOf[GenericTestEvent[string]](), "github.com/kubecost/events/GenericTestEvent[string]")
	cmp(t, typeOf[GenericTestEvent[GenericTestEvent[string]]](), "github.com/kubecost/events/GenericTestEvent[events.GenericTestEvent[string]]")
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

	// for 10 events and 3 handlers, we'll expect to receive 30 total handler calls (10
	// events per handler)
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

func TestFilteredEventStreams(t *testing.T) {
	d := NewDispatcher[TypedEvent]()

	var anyCount, oneCount, twoCount, threeCount uint32
	var wg sync.WaitGroup
	wg.Add(5)

	var goFuncGroup sync.WaitGroup
	goFuncGroup.Add(5)

	go func() {
		defer goFuncGroup.Done()

		stream := d.NewFilteredEventStream(eventFilter(TypedEventKindOne))
		wg.Done()

		for event := range stream.Stream() {
			atomic.AddUint32(&oneCount, 1)
			t.Logf("One Handler: [Event %s]\n", event.Message)
		}
	}()

	go func() {
		defer goFuncGroup.Done()

		stream := d.NewFilteredEventStream(eventFilter(TypedEventKindTwo))
		wg.Done()

		for syncEvent := range stream.SyncStream() {
			func() {
				defer syncEvent.Done()

				event := syncEvent.Event
				atomic.AddUint32(&twoCount, 1)
				t.Logf("Two Handler: [Event %s]\n", event.Message)
			}()
		}
	}()

	go func() {
		defer goFuncGroup.Done()

		stream := d.NewFilteredEventStream(eventFilter(TypedEventKindThree))
		wg.Done()

		for syncEvent := range stream.SyncStream() {
			func() {
				defer syncEvent.Done()

				event := syncEvent.Event
				atomic.AddUint32(&threeCount, 1)
				t.Logf("Three Handler: [Event %s]\n", event.Message)
			}()
		}
	}()

	go func() {
		defer goFuncGroup.Done()

		stream := d.NewFilteredEventStream(eventFilter(TypedEventKindThree))
		wg.Done()

		for syncEvent := range stream.SyncStream() {
			func() {
				defer syncEvent.Done()

				event := syncEvent.Event
				atomic.AddUint32(&threeCount, 1)
				t.Logf("Another Three Handler: [Event %s]\n", event.Message)
			}()
		}
	}()

	go func() {
		defer goFuncGroup.Done()

		stream := d.NewEventStream()
		wg.Done()

		for syncEvent := range stream.SyncStream() {
			func() {
				defer syncEvent.Done()

				event := syncEvent.Event
				atomic.AddUint32(&anyCount, 1)
				t.Logf("Universal Handler: [Event %s]\n", event.Message)
			}()
		}
	}()

	// wait until all of the event streams were added before dispatching
	wg.Wait()

	d.DispatchSync(TypedEvent{TypedEventKindOne, "One"})
	d.DispatchSync(TypedEvent{TypedEventKindTwo, "Two"})
	d.DispatchSync(TypedEvent{TypedEventKindThree, "Three"})

	d.CloseEventStreams()
	goFuncGroup.Wait()

	if anyCount != 3 {
		t.Errorf("Any Count != 3. Got: %d\n", anyCount)
	}

	if oneCount != 1 {
		t.Errorf("One Count != 1. Got: %d\n", oneCount)
	}

	if twoCount != 1 {
		t.Errorf("Two Count != 1. Got: %d\n", twoCount)
	}

	if threeCount != 2 {
		t.Errorf("Three Count != 2. Got: %d\n", threeCount)
	}
}

func TestAddFilteredHandlers(t *testing.T) {
	d := NewDispatcher[TypedEvent]()

	var handlerGroup sync.WaitGroup
	handlerGroup.Add(5)

	var handlers []HandlerID = make([]HandlerID, 5)
	var anyCount, oneCount, twoCount, threeCount uint32

	oneFilter := eventFilter(TypedEventKindOne)
	twoFilter := eventFilter(TypedEventKindTwo)
	threeFilter := eventFilter(TypedEventKindThree)

	handlers[0] = d.AddFilteredEventHandler(func(event TypedEvent) {
		atomic.AddUint32(&oneCount, 1)
		t.Logf("One Handler: [Event %s]\n", event.Message)
	}, oneFilter)

	handlers[1] = d.AddFilteredEventHandler(func(event TypedEvent) {
		atomic.AddUint32(&twoCount, 1)
		t.Logf("Two Handler: [Event %s]\n", event.Message)
	}, twoFilter)

	handlers[2] = d.AddFilteredEventHandler(func(event TypedEvent) {
		atomic.AddUint32(&threeCount, 1)
		t.Logf("Three Handler: [Event %s]\n", event.Message)
	}, threeFilter)

	handlers[3] = d.AddFilteredEventHandler(func(event TypedEvent) {
		atomic.AddUint32(&threeCount, 1)
		t.Logf("Another Three Handler: [Event %s]\n", event.Message)
	}, threeFilter)

	handlers[4] = d.AddEventHandler(func(event TypedEvent) {
		atomic.AddUint32(&anyCount, 1)
		t.Logf("Universal Handler: [Event %s]\n", event.Message)
	})

	md := d.(*multicastDispatcher[TypedEvent])
	for i := 0; i < len(handlers); i++ {
		go func(onClose chan struct{}) {
			defer handlerGroup.Done()
			<-onClose
		}(md.getHandlerClose(handlers[i]))
	}

	d.DispatchSync(TypedEvent{TypedEventKindOne, "One"})
	d.DispatchSync(TypedEvent{TypedEventKindTwo, "Two"})
	d.DispatchSync(TypedEvent{TypedEventKindThree, "Three"})

	d.CloseEventStreams()
	handlerGroup.Wait()

	if anyCount != 3 {
		t.Errorf("Any Count != 3. Got: %d\n", anyCount)
	}

	if oneCount != 1 {
		t.Errorf("One Count != 1. Got: %d\n", oneCount)
	}

	if twoCount != 1 {
		t.Errorf("Two Count != 1. Got: %d\n", twoCount)
	}

	if threeCount != 2 {
		t.Errorf("Three Count != 2. Got: %d\n", threeCount)
	}
}

func TestGlobalDispatcherCloseAllAndReuse(t *testing.T) {
	var closeGroup sync.WaitGroup
	var dispatchGroup sync.WaitGroup
	var handlerGroup sync.WaitGroup

	handlerGroup.Add(3)
	dispatchGroup.Add(3)
	closeGroup.Add(3)

	d := GlobalDispatcherFor[TestEvent]()

	for i := 0; i < 3; i++ {
		s := d.NewEventStream()
		go func(ii int, stream EventStream[TestEvent]) {
			handlerGroup.Done()

			for event := range stream.Stream() {
				t.Logf("[%d] [%s]\n", ii, event.Message)
				dispatchGroup.Done()
			}
			closeGroup.Done()
		}(i, s)
	}

	handlerGroup.Wait()

	// test events package dispatch convenience method
	Dispatch(TestEvent{Message: "Test"})
	dispatchGroup.Wait()

	d.CloseEventStreams()

	select {
	case <-time.After(5 * time.Second):
		t.Errorf("Test failed. Timed out after 5 seconds\n")
	case <-waitChannelFor(&closeGroup):
		t.Logf("Completed successfully\n")
	}

	var wg sync.WaitGroup
	wg.Add(1)
	d.AddEventHandler(func(te TestEvent) {
		t.Logf("Global Handler: [%s]\n", te.Message)
		wg.Done()
	})

	Dispatch(TestEvent{Message: "Test"})

	select {
	case <-time.After(5 * time.Second):
		t.Errorf("Test failed. Timed out after 5 seconds\n")
	case <-waitChannelFor(&wg):
		t.Logf("Completed successfully\n")
	}

	d.CloseEventStreams()
}

func TestSyncAndAsyncDispatch(t *testing.T) {
	var totalHandled uint32 = 0

	var wait sync.WaitGroup
	wait.Add(2)

	d := NewDispatcher[TestEvent]()

	go func() {
		wait.Done()

		stream := d.NewEventStream()
		for syncEvent := range stream.SyncStream() {
			func() {
				defer syncEvent.Done()
				atomic.AddUint32(&totalHandled, 1)
				t.Logf("Sync Receive: [%s]\n", syncEvent.Event.Message)
			}()
		}
	}()

	go func() {
		wait.Done()

		stream := d.NewEventStream()
		for evt := range stream.Stream() {
			atomic.AddUint32(&totalHandled, 1)
			t.Logf("Async Receive: [%s]\n", evt.Message)
		}
	}()

	wait.Wait()

	for i := 0; i < 3; i++ {
		if i != 1 {
			d.Dispatch(TestEvent{Message: fmt.Sprintf("Regular Event: %d", i)})
		} else {
			d.DispatchSync(TestEvent{Message: fmt.Sprintf("Sync Event: %d", i)})
		}
	}

	time.Sleep(1 * time.Second)
	if totalHandled != 6 {
		t.Errorf("Total Handled != 6. Got: %d\n", totalHandled)
	}
}

func TestDispatchSyncWithTimeout(t *testing.T) {
	var complete sync.WaitGroup
	complete.Add(1)

	d := NewDispatcher[TestEvent]()

	go func() {
		defer complete.Done()

		es := d.NewEventStream()
		for syncEvent := range es.SyncStream() {
			t.Logf("I messed up and forgot to call Done() on the sync event: %s!\n", syncEvent.Event.Message)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	d.DispatchSyncWithTimeout(TestEvent{Message: "Test"}, time.Second)
	d.CloseEventStreams()

	select {
	case <-waitChannelFor(&complete):
		t.Logf("Completed successfully\n")
		return
	case <-time.After(3 * time.Second):
		t.Errorf("Test failed. Timed out after 3 seconds\n")
	}
}

func TestDispatchSyncNoReceivers(t *testing.T) {
	d := NewDispatcher[TestEvent]()

	complete := make(chan struct{})
	go func() {
		d.DispatchSync(TestEvent{Message: "Test"})
		complete <- struct{}{}
	}()

	select {
	case <-complete:
		t.Logf("Completed successfully\n")
		return
	case <-time.After(1 * time.Second):
		t.Errorf("Test failed. Timed out after 1 second\n")
	}
}

func TestBlockingSyncDispatch(t *testing.T) {
	var complete sync.WaitGroup
	complete.Add(1)

	d := NewDispatcher[TestEvent]()

	go func() {
		defer complete.Done()

		es := d.NewEventStream()
		for syncEvent := range es.SyncStream() {
			func() {
				defer syncEvent.Done()

				time.Sleep(2 * time.Second)
				t.Logf("Finished waiting for sync event: %s\n", syncEvent.Event.Message)
			}()
		}
	}()

	time.Sleep(100 * time.Millisecond)

	var didBlock sync.WaitGroup
	didBlock.Add(1)

	go func() {
		d.DispatchSync(TestEvent{Message: "Test"})
		didBlock.Done()
	}()

	start := time.Now()
	select {
	case <-waitChannelFor(&didBlock):
	case <-time.After(3 * time.Second):
	}

	delta := time.Now().Sub(start)
	if delta < (2 * time.Second) {
		t.Errorf("Test failed. Blocked for less than 2 seconds: %dms\n", delta.Milliseconds())
	}
	if delta > (3 * time.Second) {
		t.Errorf("Test failed. Blocked longer than 3 seconds: %dms\n", delta.Milliseconds())
	}

	d.CloseEventStreams()

	select {
	case <-waitChannelFor(&complete):
	case <-time.After(1 * time.Second):
		t.Errorf("Test failed. Timed out after 1 second\n")
	}

}

//--------------------------------------------------------------------------
//  Benchmarks
//--------------------------------------------------------------------------

// creates the dispatcher benchmark data structures
func createDispatcherBenchmark[T any](numListeners int) (Dispatcher[T], []HandlerID, *sync.WaitGroup) {
	var wg sync.WaitGroup
	wg.Add(numListeners)

	dispatcher := NewDispatcher[T]()
	handlers := make([]HandlerID, numListeners)
	for i := 0; i < numListeners; i++ {
		handlers[i] = dispatcher.AddEventHandler(func(event T) {
			wg.Done()
		})
	}

	return dispatcher, handlers, &wg
}

// benchmark runner for a specific number of listeners
func benchmarkDispatcher(numListeners int, b *testing.B) {
	d, _, wg := createDispatcherBenchmark[TestEvent](numListeners)

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		d.Dispatch(TestEvent{"Test"})

		// wait for all listeners to trigger
		wg.Wait()

		b.StopTimer()
		// reset wait group count
		wg.Add(numListeners)
		b.StartTimer()
	}
}

func BenchmarkDispatcher5(b *testing.B) { benchmarkDispatcher(5, b) }

func BenchmarkDispatcher100(b *testing.B) { benchmarkDispatcher(100, b) }

func BenchmarkDispatcher1000(b *testing.B) { benchmarkDispatcher(1000, b) }
