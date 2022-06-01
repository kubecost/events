# Eventing API
This event API is a very simple implementation of a global dispatch which leverages generic event type multicast dispatchers. This library requires go 1.18 for generics support. 

## Dispatchers
The main component of the events API is the `Dispatcher[T]` contract, which is implemented in this library as a multicast channel based dispatch. This library creates a global table that can be used to instantiate global event dispatchers as well as a method for instancing dispatchers individually.

### Global Dispatcher Access
To access the global dispatcher for a specific type, use the following:
```go
type MyEvent struct {
    Message string 
}

myEventDispatcher := events.GlobalDispatcherFor[MyEvent]()
```

This will allow you to retrieve a global dispatcher for the type `MyEvent`. Any events that dispatch over the global instance will be received by any event handlers on that dispatcher. Global dispatchers are instanced based on the type. For example, `events.GlobalDispatcherFor[MyEvent]()` will always return the exact dispatcher instance. 

### Instanced Dispatcher 
To create a new instance of a dispatcher, use the following:
```go
type MyEvent struct {
    Message string 
}

myEventDispatcher := events.NewDispatcher[MyEvent]()
```

This will create a new dispatcher instance for the type `MyEvent`. Any events that dispatch over the instance will be received by any event handlers on that dispatcher only. Instanced dispatchers do not share event streams with the global dispatcher for that type. 

Instanced dispatchers should also be closed when they are no longer needed. This will close any open event streams and remove any existing event handlers automatically. *NOTE:* Calling `CloseEventStreams()` on an instanced dispatcher will end that dispatchers lifecycle, disallowing any reuse. Calling `CloseEventStreams()` on a global dispatcher will remove all event handlers and close all event streams, but will not shutdown the dispatcher, and will allow it to be reused. 
```go
type MyEvent struct {
    Message string 
}

// Create a new dispatcher instance
myEventDispatcher := events.NewDispatcher[MyEvent]()

// use the dispatcher... 

// shutdown the dispatcher
myEventDispatcher.CloseEventStreams()
```

## Receiving Dispatched Events
Once you create a dispatcher instance, or retrieve a global dispatcher, you can add event handlers capable of receiving events that are dispatched via two methods:

### Event Streams
Event streams are used internally in the default multicast dispatcher implementation. For channel based APIs, event streams provide direct access to the event receiving channel, so a simple range loop can be used to process all incoming events.

```go
type MyEvent struct {
    Message string 
}

dispatcher := events.NewDispatcher[MyEvent]()
stream := dispatcher.NewEventStream()

// typically, a stream will be handled in a separate goroutine
go func() {
    for event := range stream.Stream() {
        fmt.Println(event.Message)
    }
}()

// dispatch an event 
dispatcher.Dispatch(MyEvent{Message: "Hello World!"})

// Output:
// Hello World!
```

### Event Handlers
Event handlers are an abstraction over the event streams, and provide a simpler way to listen to events via function receivers.

```go
type MyEvent struct {
    Message string 
}

dispatcher := events.NewDispatcher[MyEvent]()
id := dispatcher.AddEventHandler(func(event MyEvent) {
    fmt.Println(event.Message)
})

// dispatch an event 
dispatcher.Dispatch(MyEvent{Message: "Hello World!"})

// simulate some time passing 
time.Sleep(time.Second)

// remove the event handler when it's no longer needed
dispatcher.RemoveEventHandler(id)
```

You can also add new or remove existing handlers inside an event handler. A better way to write the previous example might be:
```go
type MyEvent struct {
    Message string 
}

// handlerID should be captured by the handler 
var handlerID events.HandlerID

dispatcher := events.NewDispatcher[MyEvent]()
handlerID = dispatcher.AddEventHandler(func(event MyEvent) {
    fmt.Println(event.Message)
    dispatcher.RemoveEventHandler(handlerID)
})

// dispatch an event 
dispatcher.Dispatch(MyEvent{Message: "Hello World!"})
```

## Filtering Events 
The dispatcher supports filtering events based on a custom criteria for an event type. For example, you can have an event stream which only receives events that have a specific identifier, or events which have a `Message string` field that contains a specific substring. Let's use a similar example to the previous section, but with a filter:

### Filtered Event Stream
```go
type MyEvent struct {
    Message string 
}

dispatcher := events.NewDispatcher[MyEvent]()

// create a new event stream that only receives events with a Message that contains the substring "Wow"
stream := dispatcher.NewFilteredEventStream(func(event MyEvent) bool {
    return strings.Contains(event.Message, "Wow")
})

// typically, a stream will be handled in a separate goroutine
go func() {
    for event := range stream.Stream() {
        fmt.Println(event.Message)
    }
}()

// dispatch an event 
dispatcher.Dispatch(MyEvent{Message: "Hello World!"})
dispatcher.Dispatch(MyEvent{Message: "Hello! Wow!"})

// Output:
// Hello! Wow!
```

Note that filtered events actually prevent the event from ever dispatching over the stream, so it is more performant than adding a conditional check in a default handler for the event type.

### Filtered Event Handler
Just like with event streams, you can add a filtered event handler as well by using:
```go
type MyEvent struct {
    Message string 
}

eventHandler := func(event MyEvent) {
    fmt.Println(event.Message)
}

eventFilter := func(event MyEvent) bool { 
    return strings.Contains(event.Message, "Wow") 
}

dispatcher := events.NewDispatcher[MyEvent]()
id := dispatcher.AddFilteredEventHandler(eventHandler, eventFilter)

// dispatch an event 
dispatcher.Dispatch(MyEvent{Message: "Hello World!"})
dispatcher.Dispatch(MyEvent{Message: "Hello! Wow!"})

// simulate some time passing 
time.Sleep(time.Second)

// Remove is the same, just use the handler id provided
dispatcher.RemoveEventHandler(id)
```

### Example
The following example is meant to demonstrate a very simplified use of the dispatcher. It receives two "types" of events, and could be re-written with filters to only receive specific events for specific Count types. It also leverages the global dispatcher for `CountEvent` (Note that `SecondCounterTo` doesn't accept a dispatcher parameter). This example could also be re-written to leverage a single instanced Dispatcher instead of the global dispatcher. Making the two previously mentioned changes may be a good programming exercise to better understand the events library. 

```go

import (
    "time"
    "fmt" 

    "github.com/kubecost/events"
)

const (
    CountEventTypeCount = "count"
    CountEventTypeFinished = "finished"
)

// CountEvent is our custom event payload
type CountEvent struct {
    Type string
    Value uint 
    Timestamp time.Time
}

// Counts up every second, dispatches an event dispatchEvery seconds 
func SecondCounterTo(target uint, dispatchEvery uint) {
    var count uint = 0

    // get a reference to the dispatcher for our count event
    dispatcher := events.GlobalDispatcherFor[CountEvent]()

    // Loop until our count reaches the target 
    for {    
        time.Sleep(time.Second)
        count++ 

        // dispatch when our count is divisble by dispatchEvery 
        if count % dispatchEvery == 0 {
            dispatcher.Dispatch(CountEvent{
                Type: CountEventTypeCount,
                Value: count,
                Timestamp: time.Now(),
            })
        }

        if count == target {
            dispatcher.Dispatch(CountEvent{
                Type: CountEventTypeFinished,
                Value: count,
                Timestamp: time.Now(),
            })
            return 
        }
    }
}

// Entry point 
func main() {
    // channel used to wait for counting complete 
    complete := make(chan bool) 

    // get a reference to the global dispatcher 
    dispatcher := events.GlobalDispatcherFor[CountEvent]()

    // create a handler that receives count events and logs them if they're Count types 
    // note that each handler has it's own goroutine context, so if you need to communicate 
    // outside of the handler, you must use channels
    dispatcher.AddEventHandler(func(event CountEvent) {
        if event.Type == CountEventTypeCount {
            fmt.Printf("Count: %d\n", event.Value)
        }
    })

    // create a handler that receives count events and waits for a Finished type
    // note that each handler has it's own goroutine context, so if you need to communicate 
    // outside of the handler, you must use channels
    dispatcher.AddEventHandler(func(event CountEvent) {
        if event.Type == CountEventTypeFinished {
            complete <- true 
        }
    })

    // run our slow counter to 10, count events every 3
    go SecondCounterTo(10, 3)

    // waits until complete is signaled 
    <- complete
}
```
