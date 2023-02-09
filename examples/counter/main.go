package main

import (
	"fmt"

	"github.com/Arceliar/phony"
)

// Structs can embed the Inbox type to fulfill the Actor interface.
type printer[T any] struct {
	phony.Inbox[T]
}

// Functions can be defined to send messages to an Actor from another Actor.
func (p *printer[T]) Println(from phony.AnyActor, msg ...interface{}) {
	p.Act(from, func(_ *T) { fmt.Println(msg...) })
}

// It's useful to embed an Actor in a struct whose fields the Actor is responsible for.
type counter struct {
	phony.Inbox[int]
	printer *printer[int]
}

// Act with a nil sender is useful for asking an Actor to do something from non-Actor code.
func (c *counter) Increment() {
	c.Act(nil, func(count *int) { *count++ })
}

// Block waits until after a message has been processed before returning.
// This can be used to interrogate an Actor from an outside goroutine.
// Note that Actors shouldn't use this on eachother, since it blocks, it's just meant for convenience when interfacing with outside code.
func (c *counter) Get() int {
	var n int
	c.Block(func(count *int) { n = *count })
	return n
}

// Print sends a message to the counter, telling to to call c.printer.Println
// Calling Println sends a message to the printer, telling it to print
// So message sends become function calls.
func (c *counter) Print() {
	c.Act(nil, func(count *int) {
		c.printer.Println(c, "The count is:", *count)
	})
}

func main() {
	c := &counter{printer: new(printer[int])} // Create an actor
	for idx := 0; idx < 10; idx++ {
		c.Increment() // Ask the Actor to do some work
		c.Print()     // And ask it to send a message to another Actor, which handles them asynchronously
	}
	n := c.Get()                      // Inspect the Actor's internal state
	fmt.Println("Value from Get:", n) // This likely prints before the Print() lines above have finished -- Actors work asynchronously.
	c.printer.Block(func(_ *int) {})  // Wait for an Actor to handle a message, in this case just to finish printing
	fmt.Println("Exiting")
}
