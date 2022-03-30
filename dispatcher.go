package events

import (
	"fmt"
	"os"
	"sync"

	"github.com/google/uuid"
)

// HandlerID is a unique identifier assigned to a provided event handler. This is used to remove a handler
// from the dispatcher when it is no longer needed.
type HandlerID string

// EventHandler[T] is a type used to receive events from a dispatcher.
type EventHandler[T any] func(T)

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

	// DispatchSync is a special dispatch scenario which will block if any
	// listeners are not yet receiving events. This should be used if you
	// need to guarantee that all receivers are processing events before
	// continuing.
	DispatchSync(event T)

	// AddEventHandler adds a new event handler method that is called whenever an event T is dispatched. A
	// unique HandlerID is returned that can be used to remove the handler.
	AddEventHandler(handler EventHandler[T]) HandlerID

	// RemoveEventHandler removes an event handler that was added via AddEventHandler using it's HandlerID.
	// Returns true if the handler was successfully removed.
	RemoveEventHandler(id HandlerID) bool

	// NewEventStream returns an asynchronous event stream that can be used to receive dispatched events.
	NewEventStream() EventStream[T]
}

// asyncEventStream contains the event stream channel and metadata
type asyncEventStream[T any] struct {
	m        sync.Mutex
	stream   chan T
	isClosed bool
}

// Stream returns access to the event T channel where events will arrive.
func (aes *asyncEventStream[T]) Stream() <-chan T {
	return aes.stream
}

// Close shuts down the event stream, closing the channel
func (aes *asyncEventStream[T]) Close() {
	aes.m.Lock()
	defer aes.m.Unlock()
	if aes.isClosed {
		return
	}
	aes.isClosed = true

	close(aes.stream)
}

// IsClosed is set to true if the event stream has been closed
func (aes *asyncEventStream[T]) IsClosed() bool {
	aes.m.Lock()
	defer aes.m.Unlock()
	return aes.isClosed
}

// dispatchedEvent[T] is an event payload that is processed in the event dispatching loop.
type dispatchedEvent[T any] struct {
	event T
	sent  chan struct{}
}

// eventStreamHandler[T] represents an event handler processor linked to an EventHandler[T]
// as a result of using AddEventHandler.
type eventStreamHandler[T any] struct {
	id     HandlerID
	stream EventStream[T]
}

// multicastDispatcher[T] is a channel based multicast dispatcher
type multicastDispatcher[T any] struct {
	in  chan *dispatchedEvent[T]
	end chan struct{}

	handlers    map[HandlerID]*eventStreamHandler[T]
	handlerLock sync.Mutex

	streams    []*asyncEventStream[T]
	streamLock sync.Mutex
}

// AddEventHandler adds a new event handler method that is called whenever an event T is dispatched. A
// unique HandlerID is returned that can be used to remove the handler.
func (md *multicastDispatcher[T]) AddEventHandler(handler EventHandler[T]) HandlerID {
	stream := md.NewEventStream()

	// create a new go routine event receive loop for the new event stream
	go func() {
		for event := range stream.Stream() {
			err := md.executeHandler(handler, event)

			// TODO: Pipe any errors that occur to an external handler rather than
			// TODO: dumping directly to stderr
			if err != nil {
				fmt.Fprintf(os.Stderr, "EventHandler Error: %s\n", err)
			}
		}
	}()

	handlerID := HandlerID(uuid.NewString())
	sHandler := &eventStreamHandler[T]{
		id:     handlerID,
		stream: stream,
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

// DispatchSync is a special dispatch scneario which will block if any
// listeners are not yet receiving events. This should be used if you
// need to guarantee that all receivers are processing events before
// continuing.
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

func (md *multicastDispatcher[T]) NewEventStream() EventStream[T] {
	md.streamLock.Lock()
	defer md.streamLock.Unlock()

	aes := &asyncEventStream[T]{
		isClosed: false,
		stream:   make(chan T),
	}

	md.streams = append(md.streams, aes)
	return aes
}

// newMulticastDispatcher creates a new Dispatcher[T]
func newMulticastDispatcher[T any]() Dispatcher[T] {
	in := make(chan *dispatchedEvent[T], 5)
	end := make(chan struct{})

	md := &multicastDispatcher[T]{
		in:       in,
		end:      end,
		handlers: make(map[HandlerID]*eventStreamHandler[T]),
	}

	go func() {
		for {
			var evt *dispatchedEvent[T]

			// Select on incoming event or a shutdown
			select {
			case evt = <-md.in:
			case <-md.end:
				md.streamLock.Lock()
				for i := 0; i < len(md.streams); i++ {
					md.streams[i].Close()
				}
				md.streamLock.Unlock()
				return
			}

			// get the event streams to notify
			streams := md.getEventStreams()
			if len(streams) == 0 {
				continue
			}

			// check to see if the event sent over requires synchronization or not
			if evt.sent == nil {
				md.executeAsync(streams, evt)
			} else {
				md.executeSync(streams, evt)
			}
		}
	}()

	return md
}

// getEventStreams returns an event stream list to notify.
func (md *multicastDispatcher[T]) getEventStreams() []*asyncEventStream[T] {
	md.streamLock.Lock()
	if len(md.streams) == 0 {
		md.streamLock.Unlock()
		return nil
	}

	// ensure that we remove all streams that are closed
	// little note about this logic: we create a pointer to the
	// same underlying array for ad.streams, then write only valid
	// streams, then finally nil out any of the remaining indices
	// and set the new streams pointer (it reuses the same memory to
	// filter out closed streams)
	validStreams := md.streams[:0]
	for _, stream := range md.streams {
		if !stream.IsClosed() {
			validStreams = append(validStreams, stream)
		}
	}

	// nil out any leftover indexes, and update the slice
	for i := len(validStreams); i < len(md.streams); i++ {
		md.streams[i] = nil
	}
	md.streams = validStreams

	// TODO: We should pool these scratch array copies so we don't have to
	// TODO: allocate every time.
	toNotify := make([]*asyncEventStream[T], len(md.streams))
	copy(toNotify, md.streams)
	md.streamLock.Unlock()

	return toNotify
}

// executeAsync will create go routines to send the event to each stream and does not block
// when a receiver doesn't exist.
func (md *multicastDispatcher[T]) executeAsync(streams []*asyncEventStream[T], evt *dispatchedEvent[T]) {
	for i := 0; i < len(streams); i++ {
		go func(stream *asyncEventStream[T]) {
			defer func() {
				if r := recover(); r != nil {
					// FIXME: This will happen if events for the stream are queued and the stream is then closed.
					// FIXME: Occurs due to a conflict caused by our design (allowing handlers to be externally
					// FIXME: closed, which breaks go channel principles). We should look into a way to maintain
					// FIXME: go principles and maintain our API.
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

	for i := 0; i < len(streams); i++ {
		go func(stream *asyncEventStream[T]) {
			defer wg.Done()

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
