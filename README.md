# Eventing API
This event API is a very simple implementation of a global dispatch which leverages generic event type multicast dispatchers. This library requires go 1.18 for generics support. 

## Basic Use 

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
    dispatcher := events.DispatcherFor[CountEvent]()

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

    // get a reference to our dispatcher 
    dispatcher := events.DispatcherFor[CountEvent]()

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
