package phony

import (
	"runtime"
	"sync"
	"sync/atomic"
)

var stops = sync.Pool{New: func() interface{} { return make(chan struct{}, 1) }}

// A message in the queue
type queueElem[T any] struct {
	msg  func(t *T)
	next atomic.Pointer[queueElem[T]] // *queueElem, accessed atomically
}

// Inbox is an ordered queue of messages which an Actor will process sequentially.
// Messages are meant to be in the form of non-blocking functions of 0 arguments, often closures.
// The intent is for the Inbox struct to be embedded in other structs, causing them to satisfy the Actor interface, and then the Actor is used to access any protected fields of the struct.
// It is up to the user to ensure that memory is used safely, and that messages do not contain blocking operations.
// An Inbox must not be copied after first use.
type Inbox[T any] struct {
	noCopy noCopy
	inner  T                            // Initialised automatically
	head   *queueElem[T]                // Used carefully to avoid needing atomics
	tail   atomic.Pointer[queueElem[T]] // *queueElem, accessed atomically
	busy   atomic.Bool                  // accessed atomically, 1 if sends should apply backpressure
	pool   *sync.Pool                   // Initialised on first enqueue
}

// Actor is the interface for Actors, based on their ability to receive a message from another Actor.
// It's meant so that structs which embed an Inbox can satisfy a mutually compatible interface for message passing.
type Actor[T any] interface {
	Act(Actor[T], func(t *T))
	Block(func(t *T))
	enqueue(func(t *T))
	restart()
	advance() bool
	AnyActor
}

// AnyActor represents any actor to which backpressure can be applied when passed to Act.
// An Actor[T] of any type T conforms to this interface automatically.
type AnyActor interface {
	enqueueSignal(done chan struct{})
	enqueueWait(done chan struct{})
}

// enqueue puts a message into the Inbox and returns true if backpressure should be applied.
// If the inbox was empty, then the actor was not already running, so enqueue starts it.
func (a *Inbox[T]) enqueue(msg func(inner *T)) {
	if a.pool == nil {
		a.pool = &sync.Pool{New: func() interface{} { return new(queueElem[T]) }}
	}
	q := a.pool.Get().(*queueElem[T])
	*q = queueElem[T]{msg: msg}
	tail := a.tail.Swap(q)
	if tail != nil {
		//An old tail exists, so update its next pointer to reference q
		tail.next.Store(q)
	} else {
		// No old tail existed, so no worker is currently running
		// Update the head to point to q, then start the worker
		a.head = q
		a.restart()
	}
}

// Act adds a message to an Inbox, which will be executed by the inbox's Actor at some point in the future.
// When one Actor sends a message to another, the sender is meant to provide itself as the first argument to this function.
// If the sender argument is non-nil and the receiving Inbox has been flooded, then backpressure is applied to the sender.
// This backpressue cause the sender stop processing messages at some point in the future until the receiver has caught up with the sent message.
// A nil first argument is valid, but should only be used in cases where backpressure is known to be unnecessary, such as when an Actor sends a message to itself or sends a response to a request (where it's the request sender's fault if they're flooded by responses).
func (a *Inbox[T]) Act(from AnyActor, action func(inner *T)) {
	if action == nil {
		panic("tried to send nil action")
	}
	a.enqueue(action)
	if from != nil && a.busy.Load() {
		done := stops.Get().(chan struct{})
		a.enqueueSignal(done)
		from.enqueueWait(done)
	}
}

// enqueueSignal adds a message to the inbox which signals the other actor that the message was processed.
func (a *Inbox[T]) enqueueSignal(done chan struct{}) {
	a.enqueue(func(t *T) {
		done <- struct{}{}
	})
}

// enqueueWait adds a message to the inbox which waits for the other actor to signal that the message was processed.
func (a *Inbox[T]) enqueueWait(stop chan struct{}) {
	a.enqueue(func(t *T) {
		<-stop
		stops.Put(stop)
	})
}

// Block adds a message to an Actor's Inbox, which will be executed at some point in the future.
// It then blocks until the Actor has finished running the provided function.
// Block meant exclusively as a convenience function for non-Actor code to send messages and wait for responses.
// If an Actor calls Block, then it may cause a deadlock, so Act should always be used instead.
func (a *Inbox[T]) Block(action func(t *T)) {
	if a == nil {
		panic("tried to send to nil actor")
	} else if action == nil {
		panic("tried to send nil action")
	}
	done := stops.Get().(chan struct{})
	a.enqueue(action)
	a.enqueueWait(done)
	<-done
	stops.Put(done)
}

// run is executed when a message is placed in an empty Inbox, and launches a worker goroutine.
// The worker goroutine processes messages from the Inbox until empty, and then exits.
func (a *Inbox[T]) run() {
	a.busy.Store(true)
	for running := true; running; running = a.advance() {
		a.head.msg(&a.inner)
	}
}

// returns true if we still have more work to do
func (a *Inbox[T]) advance() (more bool) {
	head := a.head
	a.head = head.next.Load()
	if a.head == nil {
		// We loaded the last message
		// Unset busy and CAS the tail to nil to shut down
		a.busy.Store(false)
		if !a.tail.CompareAndSwap(head, nil) {
			// Someone pushed to the list before we could CAS the tail to shut down
			// This means we're effectively restarting at this point
			// Set busy and load the next message
			a.busy.Store(true)
			for a.head == nil {
				// Busy loop until the message is successfully loaded
				// Gosched to avoid blocking the thread in the mean time
				runtime.Gosched()
				a.head = head.next.Load()
			}
			more = true
		}
	} else {
		more = true
	}
	*head = queueElem[T]{}
	a.pool.Put(head)
	return
}

func (a *Inbox[T]) restart() {
	go a.run()
}

// noCopy implements the sync.Locker interface, so go vet can catch unsafe copying
type noCopy struct{}

func (n *noCopy) Lock()   {}
func (n *noCopy) Unlock() {}
