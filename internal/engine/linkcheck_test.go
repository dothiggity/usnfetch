package engine

import (
	"bufio"
	"net"
	"strings"
	"testing"

	"usnfetch/internal/nntp"
)

// fakeNews: a news server that has exactly the articles in has (STAT only).
func fakeNews(t *testing.T, has map[string]bool) *nntp.Account {
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
				r := bufio.NewReader(c)
				c.Write([]byte("200 fake news\r\n"))
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
					switch strings.ToUpper(cmd) {
					case "STAT":
						if has[strings.Trim(arg, "<>")] {
							c.Write([]byte("223 0 " + arg + "\r\n"))
						} else {
							c.Write([]byte("430 no such article\r\n"))
						}
					case "QUIT":
						c.Write([]byte("205 bye\r\n"))
						return
					default:
						c.Write([]byte("500 what?\r\n"))
					}
				}
			}()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return &nntp.Account{Name: "fake", Server: "127.0.0.1", Port: addr.Port, Connections: 4}
}

func TestCheckLinks(t *testing.T) {
	primary := fakeNews(t, map[string]bool{"a@x": true, "c2@x": true})
	backup := fakeNews(t, map[string]bool{"b@x": true})
	backup.Priority = 1
	accts, err := NewAccountSet([]*nntp.Account{primary, backup})
	if err != nil {
		t.Fatal(err)
	}
	manifests := map[string][]string{
		"a": {"a@x"},          // at the primary
		"b": {"b@x"},          // only at the backup
		"c": {"c1@x", "c2@x"}, // a copy of it at the primary
		"d": {"d@x"},          // nowhere: the share is gone
		"e": {"e1@x", "e2@x"}, // nowhere either
	}
	var calls int
	res := CheckLinks(manifests, accts, 3, func(int) { calls++ })
	for k, want := range map[string]bool{"a": true, "b": true, "c": true, "d": false, "e": false} {
		if r := res[k]; r.Found != want || r.Err != nil {
			t.Errorf("%s: found %v err %v, want found %v", k, r.Found, r.Err, want)
		}
	}
	if calls != len(manifests) {
		t.Errorf("progress called %d times, want %d", calls, len(manifests))
	}

	// a provider that can't be reached: not "gone", unknown
	down := &nntp.Account{Name: "down", Server: "127.0.0.1", Port: 1, Connections: 2}
	accts, _ = NewAccountSet([]*nntp.Account{down})
	res = CheckLinks(map[string][]string{"a": {"a@x"}}, accts, 2, nil)
	if r := res["a"]; r.Found || r.Err == nil {
		t.Errorf("unreachable provider: found %v err %v, want an error", r.Found, r.Err)
	}
}
