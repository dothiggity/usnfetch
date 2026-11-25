package engine

import (
	"sync"
	"testing"
	"time"

	"usnfetch/internal/nntp"
)

// More fetches at once than the provider allows, in one pool (previews with a small plan):
// a connection handed back must go to a dial waiting for a place, not sit idle holding it.
func TestPoolHandsOverToWaitingDial(t *testing.T) {
	old := nntp.OtherWait
	nntp.OtherWait = 2 * time.Second
	t.Cleanup(func() { nntp.OtherWait = old })
	a := fakeNews(t, map[string]bool{})
	a.Connections = 2
	accts, err := NewAccountSet([]*nntp.Account{a})
	if err != nil {
		t.Fatal(err)
	}
	p := NewPool(accts)
	defer p.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	t0 := time.Now()
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				c, err := p.Get(nil)
				if err != nil {
					errs <- err
					return
				}
				if _, err := c.Stat("x@y"); err != nil {
					errs <- err
				}
				time.Sleep(20 * time.Millisecond)
				p.Put(c)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if d := time.Since(t0); d > 1500*time.Millisecond {
		t.Errorf("20 fetches over 2 connections took %v: a waiting dial sat out its time", d)
	}
	if used, _ := nntp.InUse(a); used > 2 {
		t.Errorf("%d connections open, the limit is 2", used)
	}
}

// A connection left idle in a pool gives its place back once it is past IdleMax, even when
// nothing asks the pool for another one.
func TestPoolReapsIdle(t *testing.T) {
	a := fakeNews(t, map[string]bool{})
	a.Connections = 3
	accts, _ := NewAccountSet([]*nntp.Account{a})
	p := NewPool(accts)
	p.idleMax = 100 * time.Millisecond // this pool's own: other tests' pools may still be reaping
	defer p.Close()
	c, err := p.Get(nil)
	if err != nil {
		t.Fatal(err)
	}
	p.Put(c)
	if used, _ := nntp.InUse(a); used != 1 {
		t.Fatalf("in use %d after Put", used)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if used, _ := nntp.InUse(a); used == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("an idle connection kept its place past IdleMax")
}
