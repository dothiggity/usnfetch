package nntp

import (
	"bufio"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// greeter: a news server that greets, and says goodbye on QUIT.
func greeter(t *testing.T) *Account {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.Write([]byte("200 hello\r\n"))
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if strings.HasPrefix(strings.ToUpper(line), "QUIT") {
						c.Write([]byte("205 bye\r\n"))
						return
					}
					c.Write([]byte("500 what?\r\n"))
				}
			}()
		}
	}()
	return &Account{Name: "fake", Server: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Connections: 3}
}

func TestBudget(t *testing.T) {
	old1, old2 := BulkWait, OtherWait
	BulkWait, OtherWait = 300*time.Millisecond, 3*time.Second
	t.Cleanup(func() { BulkWait, OtherWait = old1, old2 })
	a := greeter(t)

	var bulk []*Conn
	for i := 0; i < 3; i++ {
		c, err := DialBulk(a, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		bulk = append(bulk, c)
	}
	if used, limit := InUse(a); used != 3 || limit != 3 {
		t.Fatalf("in use %d of %d", used, limit)
	}
	if _, err := DialBulk(a, 5*time.Second); !errors.Is(err, ErrBusy) {
		t.Fatalf("a 4th download connection: %v", err)
	}
	if bulk[0].Yield() {
		t.Error("yield with nothing waiting")
	}

	// a preview waits; downloads see they should give way, and a new download can't jump in
	got := make(chan *Conn, 1)
	go func() {
		c, err := Dial(a, 5*time.Second)
		if err != nil {
			t.Error(err)
		}
		got <- c
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !bulk[0].Yield() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !bulk[0].Yield() {
		t.Fatal("downloads were not asked to give way")
	}
	bulk[0].Close()
	bulk[0].Close() // twice: gives its place back once
	var preview *Conn
	select {
	case preview = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the preview did not get the connection a download gave back")
	}
	if used, _ := InUse(a); used != 3 {
		t.Errorf("in use %d after the hand-over, want 3", used)
	}
	if preview.Yield() {
		t.Error("a preview's connection asked to yield")
	}
	preview.Close()
	c, err := DialBulk(a, 5*time.Second) // the place is the downloads' again
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	for _, c := range bulk[1:] {
		c.Close()
	}
	if used, _ := InUse(a); used != 0 {
		t.Errorf("in use %d after closing everything", used)
	}

	// a higher limit (the setting changed) lets more in
	a.Connections = 5
	var more []*Conn
	for i := 0; i < 5; i++ {
		c, err := DialBulk(a, 5*time.Second)
		if err != nil {
			t.Fatalf("connection %d of 5: %v", i+1, err)
		}
		more = append(more, c)
	}
	for _, c := range more {
		c.Close()
	}
}
