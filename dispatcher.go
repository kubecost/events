package events

import (
	"fmt"
	"os"
	"sync"

	"github.com/google/uuid"
)

//--------------------------------------------------------------------------
//  Alias Types
//--------------------------------------------------------------------------

// HandlerID is a unique identifier assigned to a provided event handler. This is used to remove a handler
// from the dispatcher when it is no longer needed.
type HandlerID string

// EventHandler[T] is a type used to receive events from a dispatcher.
type EventHandler[T any] func(T)

// EventCondition[T] is a type used to filter events that are dispatched to a specific handler.
type EventCondition[T any] func(T) bool

//--------------------------------------------------------------------------
//  Contracts
//--------------------------------------------------------------------------

// EventStream[T] is an implementation prototype for an object capable of asynchronously
// listening for events dispatched via Dispatcher[T] implementation.
type EventStream[T any] interface {
	// Stream returns access to the event T channel where events will arrive.
	Stream() <-chan T

	// Close shuts down the event stream, closing the channel
	Close()

	// IsClosed is set to true if the event stream has been closed
	IsClosed() bool
}

// Dispatcher[T] is an implementation prototype for an object capable of dispatching T
// instances to any subscribed listeners.
type Dispatcher[T any] interface {
	// Dispatch broadcasts the T event to any subscribed listeners asynchronously.
	Dispatch(event T)

	// DispatchSync is a special dispatch scenario which will block if any listeners are not yet receiving
	// events. This should be used if you need to guarantee that all receivers are processing events before
	// continuing. This method also has the added benefit of blocking until an event has been dispatched
	// over all event streams before returning.
	DispatchSync(event T)

	// AddEventHandler adds a new event handler method that is called whenever an event T is dispatched. A
	// unique HandlerID is returned that can be used to remove the handler.
	AddEventHandler(handler EventHandler[T]) HandlerID

	// AddFilteredEventHandler adds a new event handler method that is called whenever a dispatched event T
	// passes the provided condition. A unique HandlerID is returned that can be used to remove the handler.
	// Note that the condition will be checked prior to dispatch, which is more performant than filtering
	// in the event handler itself.
	AddFilteredEventHandler(handler EventHandler[T], condition EventCondition[T]) HandlerID

	// RemoveEventHandler removes an event handler that was added via AddEventHandler using it's HandlerID.
	// Returns true if the handler was successfully removed.
	RemoveEventHandler(id HandlerID) bool

	// NewEventStream returns an asynchronous event stream that can be used to receive dispatched events.
	NewEventStream() EventStream[T]

	// NewFilteredEventStream creates a new event stream that will only receive events that match the provided
	// condition. Note that the condition will be checked prior to dispatch, which is more performant than
	// filtering in the event handler itself.
	NewFilteredEventStream(condition EventCondition[T]) EventStream[T]

	// CloseEventStreams closes all listening event streams and shuts down the dispatcher
	CloseEventStreams()
}

//--------------------------------------------------------------------------
//  Default EventStream[T] Implementation
//--------------------------------------------------------------------------

// asyncEventStream contains the event stream channel and metadata
type asyncEventStream[T any] struct {
	stream    chan T
	closed    *atomicbool
	condition EventCondition[T]
}

// newAsyncEventStream creates a new asynchronous event stream for a listener.
func newAsyncEventStream[T any](condition EventCondition[T]) *asyncEventStream[T] {
	return &asyncEventStream[T]{
		closed:    newAtomicBool(false),
		stream:    make(chan T),
		condition: condition,
	}
}

// Stream returns access to the event T channel where events will arrive.
func (aes *asyncEventStream[T]) Stream() <-chan T {
	return aes.stream
}

// Close shuts down the event stream, closing the channel
func (aes *asyncEventStream[T]) Close() {
	if !aes.closed.CompareAndSet(false, true) {
		return
	}

	close(aes.stream)
}

// IsClosed is set to true if the event stream has been closed
func (aes *asyncEventStream[T]) IsClosed() bool {
	return aes.closed.Get()
}

//--------------------------------------------------------------------------
//  Dispatcher Support Types
//--------------------------------------------------------------------------

// dispatchedEvent[T] is an event payload that is processed in the event dispatching loop.
type dispatchedEvent[T any] struct {
	event T
	sent  chan struct{}
}

// closeEvent is used to communicate to the dispatcher to shutdown any existing event streams
type closeEvent struct {
	done chan struct{}
}

// eventStreamHandler[T] represents an event handler processor linked to an EventHandler[T]
// as a result of using AddEventHandler.
type eventStreamHandler[T any] struct {
	id      HandlerID
	stream  EventStream[T]
	onClose chan struct{}
}

//--------------------------------------------------------------------------
//  Default Dispatcher[T] Implementation
//--------------------------------------------------------------------------

// multicastDispatcher[T] is a channel based multicast dispatcher
type multicastDispatcher[T any] struct {
	in  chan *dispatchedEvent[T]
	end chan *closeEvent

	handlers    map[HandlerID]*eventStreamHandler[T]
	handlerLock sync.Mutex

	streams set[*asyncEventStream[T]]
}

// AddEventHandler adds a new event handler method that is called whenever an event T is dispatched. A
// unique HandlerID is returned that can be used to remove the handler.
func (md *multicastDispatcher[T]) AddEventHandler(handler EventHandler[T]) HandlerID {
	return md.AddFilteredEventHandler(handler, nil)
}

// AddFilteredEventHandler adds a new event handler method that is called whenever a dispatched event T
// passes the provided condition. A unique HandlerID is returned that can be used to remove the handler.
func (md *multicastDispatcher[T]) AddFilteredEventHandler(handler EventHandler[T], condition EventCondition[T]) HandlerID {
	handlerID := HandlerID(uuid.NewString())
	stream := md.NewFilteredEventStream(condition)
	onClose := make(chan struct{})

	// create a new go routine event receive loop for the new event stream
	go func(id HandlerID) {
		defer close(onClose)

		for event := range stream.Stream() {
			err := md.executeHandler(handler, event)

			// TODO: Pipe any errors that occur to an external handler rather than
			// TODO: dumping directly to stderr
			if err != nil {
				fmt.Fprintf(os.Stderr, "EventHandler Error: %s\n", err)
			}
		}

		// in the case the handler stream is closed via the dispatcher, the
		// handler will still exist, so we'll need to remove here. If the
		// handler was already removed, this will no-op.
		md.handlerLock.Lock()
		delete(md.handlers, id)
		md.handlerLock.Unlock()
	}(handlerID)

	sHandler := &eventStreamHandler[T]{
		id:      handlerID,
		stream:  stream,
		onClose: onClose,
	}

	md.handlerLock.Lock()
	md.handlers[handlerID] = sHandler
	md.handlerLock.Unlock()

	return handlerID
}

// RemoveEventHandler removes an event handler that was added via AddEventHandler using it's HandlerID.
// Returns true if the handler was successfully removed.
func (md *multicastDispatcher[T]) RemoveEventHandler(id HandlerID) bool {
	md.handlerLock.Lock()
	handler, ok := md.handlers[id]
	if !ok {
		md.handlerLock.Unlock()
		return false
	}

	delete(md.handlers, id)
	md.handlerLock.Unlock()

	// close the stream to prevent events from streaming, which will also release the
	// processing goroutine
	handler.stream.Close()
	return true
}

// DispatchSync is a special dispatch scenario which will block if any listeners are not yet receiving
// events. This should be used if you need to guarantee that all receivers are processing events before
// continuing. This method also has the added benefit of blocking until an event has been dispatched
// over all event streams before returning.
func (md *multicastDispatcher[T]) DispatchSync(event T) {
	sent := make(chan struct{})

	md.in <- &dispatchedEvent[T]{
		event: event,
		sent:  sent,
	}

	<-sent
}

// Dispatch executes an asynchronous dispatch of the provided T event.
func (md *multicastDispatcher[T]) Dispatch(event T) {
	md.in <- &dispatchedEvent[T]{
		event: event,
	}
}

// NewEventStream returns an asynchronous event stream that can be used to receive dispatched events.
func (md *multicastDispatcher[T]) NewEventStream() EventStream[T] {
	return md.NewFilteredEventStream(nil)
}

// NewFilteredEventStream creates a new event stream that will only receive events that match the provided
// condition.
func (md *multicastDispatcher[T]) NewFilteredEventStream(condition EventCondition[T]) EventStream[T] {
	aes := newAsyncEventStream(condition)
	md.streams.Add(aes)
	return aes
}

// CloseEventStreams closes all listening event streams and shuts down the dispatcher
func (md *multicastDispatcher[T]) CloseEventStreams() {
	done := make(chan struct{})
	md.end <- &closeEvent{
		done: done,
	}
	<-done
}

// newMulticastDispatcher creates a new Dispatcher[T]
func newMulticastDispatcher[T any]() Dispatcher[T] {
	in := make(chan *dispatchedEvent[T], 20)
	end := make(chan *closeEvent)

	md := &multicastDispatcher[T]{
		in:       in,
		end:      end,
		streams:  newSet[*asyncEventStream[T]](),
		handlers: make(map[HandlerID]*eventStreamHandler[T]),
	}

	go func() {
		for {
			// Select on incoming event or a shutdown
			select {
			// incoming event
			case evt := <-md.in:
				// get the event streams to notify
				streams := md.getFilteredEventStreams(evt.event)
				if len(streams) == 0 {
					continue
				}

				// check to see if the event sent over requires synchronization or not
				if evt.sent == nil {
					md.executeAsync(streams, evt)
				} else {
					md.executeSync(streams, evt)
				}

			// dispatcher closing
			case closeEvt := <-md.end:
				streams := md.streams.ToSlice()
				for _, s := range streams {
					s.Close()
				}
				md.streams.RemoveAll()
				closeEvt.done <- struct{}{}
				return
			}

		}
	}()

	return md
}

// getEventStreams returns an event stream list to notify.
func (md *multicastDispatcher[T]) getEventStreams() []*asyncEventStream[T] {
	if md.streams.Length() == 0 {
		return nil
	}

	// ensure that we remove all streams that are closed
	md.streams.RemoveOn(func(stream *asyncEventStream[T]) bool {
		return stream == nil || stream.IsClosed()
	})

	// return a slice containing the streams to dispatch to
	return md.streams.ToSlice()
}

// getFilteredEventStreams returns a slice of event streams that match conditions for the provided
// event
func (md *multicastDispatcher[T]) getFilteredEventStreams(event T) []*asyncEventStream[T] {
	if md.streams.Length() == 0 {
		return nil
	}

	// ensure that we remove all streams that are closed
	md.streams.RemoveOn(func(stream *asyncEventStream[T]) bool {
		return stream == nil || stream.IsClosed()
	})

	// return a slice containing the streams to dispatch to
	return md.streams.Filtered(func(stream *asyncEventStream[T]) bool {
		return stream.condition == nil || stream.condition(event)
	})
}

// getHandlerClose is a helper function which allows tests to access the onClose channel for
// specific handlers.
func (md *multicastDispatcher[T]) getHandlerClose(id HandlerID) chan struct{} {
	md.handlerLock.Lock()
	defer md.handlerLock.Unlock()

	if handler, ok := md.handlers[id]; ok {
		return handler.onClose
	}

	return nil
}

// executeAsync will create go routines to send the event to each stream and does not block
// when a receiver doesn't exist.
func (md *multicastDispatcher[T]) executeAsync(streams []*asyncEventStream[T], evt *dispatchedEvent[T]) {
	for i := 0; i < len(streams); i++ {
		go func(stream *asyncEventStream[T]) {
			defer func() {
				if r := recover(); r != nil {
					// NOTE: This will happen if events for the stream are queued and the stream is then closed.
					// NOTE: Occurs due to a conflict caused by our design (allowing handlers to be externally
					// NOTE: closed, which breaks go channel principles). We should look into a way to maintain
					// NOTE: go principles and maintain our API.
				}
			}()

			// stream can still be closed after this check, so we use the panic recover above for the last line
			// of defense
			if stream.IsClosed() {
				return
			}

			stream.stream <- evt.event
		}(streams[i])
	}
}

// executeSync will create go routines to send the event to each stream and _blocks_ when a receiver
// doesn't exist.
func (md *multicastDispatcher[T]) executeSync(streams []*asyncEventStream[T], evt *dispatchedEvent[T]) {
	length := len(streams)

	var wg sync.WaitGroup
	wg.Add(length)

	for i := 0; i < length; i++ {
		go func(stream *asyncEventStream[T]) {
			defer func() {
				if r := recover(); r != nil {
					// NOTE: This will happen if events for the stream are queued and the stream is then closed.
					// NOTE: Occurs due to a conflict caused by our design (allowing handlers to be externally
					// NOTE: closed, which breaks go channel principles). We should look into a way to maintain
					// NOTE: go principles and maintain our API.
				}
				wg.Done()
			}()

			if stream.IsClosed() {
				return
			}

			stream.stream <- evt.event
		}(streams[i])
	}

	wg.Wait()

	// notifies the dispatch call that all streams have been notified
	evt.sent <- struct{}{}
}

// executeHandler provides a sandbox for handlers to execute, allowing panics that occur in handlers to be
// caught and propagated as errors.
func (md *multicastDispatcher[T]) executeHandler(handler EventHandler[T], event T) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else if s, ok := r.(string); ok {
				err = fmt.Errorf("Unexpected panic: %s", s)
			} else {
				err = fmt.Errorf("Unexpected panic: %+v", r)
			}
		}
	}()

	handler(event)
	return nil
}
