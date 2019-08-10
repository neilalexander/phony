package gony

import (
	"testing"
)

func TestAct(t *testing.T) {
	var a Actor
	done := make(chan struct{})
	var results []int
	for idx := 0; idx < 16; idx++ {
		n := idx // Because idx gets mutated in place
		a.Act(func() {
			results = append(results, n)
		})
	}
	a.Act(func() { close(done) })
	<-done
	for idx, n := range results {
		if n != idx {
			t.Errorf("value %d != index %d", n, idx)
		}
	}
}

func BenchmarkAct(b *testing.B) {
	var a Actor
	f := func() {}
	for i := 0; i < b.N; i++ {
		a.Act(f)
	}
	done := make(chan struct{})
	a.Act(func() { close(done) })
	<-done
}

func TestSendMessageTo(t *testing.T) {
	var a Actor
	done := make(chan struct{})
	var results []int
	for idx := 0; idx < 16; idx++ {
		n := idx // Becuase idx gets mutated in place
		a.SendMessageTo(&a, func() {
			results = append(results, n)
		})
	}
	a.SendMessageTo(&a, func() { close(done) })
	<-done
	for idx, n := range results {
		if n != idx {
			t.Errorf("value %d != index %d", n, idx)
		}
	}
}

func BenchmarkSendMessageTo(b *testing.B) {
	var a Actor
	f := func() {}
	for i := 0; i < b.N; i++ {
		a.SendMessageTo(&a, f)
	}
	done := make(chan struct{})
	a.SendMessageTo(&a, func() { close(done) })
	<-done
}

func TestBackpressure(t *testing.T) {
	var a, b, o Actor
	// o is just some dummy observer we use to put messages on a/b queues
	ch_a := make(chan struct{})
	ch_b := make(chan struct{})
	// Block b to make sure pressure builds
	o.SendMessageTo(&b, func() { <-ch_b })
	// Have A spam messages to B
	for idx := 0; idx < 1024; idx++ {
		o.SendMessageTo(&a, func() {
			// The inner call happens in A's worker
			a.SendMessageTo(&b, func() {})
		})
	}
	o.SendMessageTo(&a, func() { close(ch_a) })
	// ch_a should now be blocked until ch_b closes
	close(ch_b)
	<-ch_a
}
