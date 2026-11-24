// Package nntp is a minimal NNTP client: connect (TLS optional), AUTHINFO, BODY and STAT
// by Message-ID. One Conn is used by one goroutine at a time.
package nntp

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrMissing: the server does not have the article (430/423/420).
var ErrMissing = errors.New("article not found")

// Account is one Usenet account.
type Account struct {
	Name        string `json:"name"`
	Server      string `json:"server"`
	Port        int    `json:"port"`
	SSL         bool   `json:"ssl"`
	User        string `json:"username"`
	Password    string `json:"password"`
	Connections int    `json:"connections"`
	Priority    int    `json:"priority"` // 0 = highest; a lower priority is only asked for what the higher ones lack
	Backup      bool   `json:"backup"`   // set by engine.NewAccountSet: not in the top priority group
}

func (a *Account) Label() string {
	if a.Name != "" {
		return a.Name
	}
	return fmt.Sprintf("%s:%d", a.Server, a.Port)
}

// Conn is one NNTP connection.
type Conn struct {
	Account *Account
	c       net.Conn
	r       *bufio.Reader
	timeout time.Duration
	bud     *budget // its place in the account's connection budget (budget.go)
	bulk    bool    // a download's connection: gives way to other uses (Yield)
	closed  sync.Once
}

// LoginError: the server turned the username or password down.
type LoginError struct{ Server, Reply string }

func (e *LoginError) Error() string {
	return fmt.Sprintf("%s rejected the username or password (%s)", e.Server, e.Reply)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// dialErr says in plain words why connecting failed, and what to check.
func dialErr(a *Account, err error) error {
	var dns *net.DNSError
	switch {
	case errors.As(err, &dns):
		return fmt.Errorf("can't find the server %s: check its name", a.Server)
	case isTimeout(err):
		return fmt.Errorf("no answer from %s on port %d: check the server name and port", a.Server, a.Port)
	case errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(err.Error(), "refused"):
		return fmt.Errorf("%s refused the connection on port %d: check the port (usually 563 with SSL, 119 without)", a.Server, a.Port)
	}
	return fmt.Errorf("can't connect to %s on port %d: %v", a.Server, a.Port, err)
}

// Dial connects, reads the greeting and logs in, within the account's connection budget
// (waiting up to OtherWait for a place; downloads give way meanwhile).
func Dial(a *Account, timeout time.Duration) (*Conn, error) { return dialIn(a, timeout, false) }

// DialBulk is Dial for a download: it may take the whole budget but gives way to anything
// else, waiting up to BulkWait for a place (ErrBusy after that: retry).
func DialBulk(a *Account, timeout time.Duration) (*Conn, error) { return dialIn(a, timeout, true) }

func dialIn(a *Account, timeout time.Duration, bulk bool) (*Conn, error) {
	b, wait := budgetOf(a), OtherWait
	if bulk {
		wait = BulkWait
	}
	if !b.acquire(bulk, wait) {
		return nil, fmt.Errorf("%w (%s: %d)", ErrBusy, a.Label(), max(1, a.Connections))
	}
	c, err := connect(a, timeout)
	if err != nil {
		b.release()
		return nil, err
	}
	c.bud, c.bulk = b, bulk
	runtime.SetFinalizer(c, (*Conn).Close) // a connection dropped without Close still gives its place back
	return c, nil
}

// Yield: a download's connection that should be handed back now, because something else is
// waiting for a place in the budget.
func (c *Conn) Yield() bool { return c.bulk && c.bud != nil && c.bud.wanted() }

// Contended: some dial is waiting for a place in this connection's budget, so a connection
// that is done should be closed rather than kept idle (an idle one holds its place).
func (c *Conn) Contended() bool { return c.bud != nil && c.bud.contended() }

func connect(a *Account, timeout time.Duration) (*Conn, error) {
	addr := net.JoinHostPort(a.Server, fmt.Sprint(a.Port))
	raw, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, dialErr(a, err)
	}
	if tc, ok := raw.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(4 << 20)
	}
	var c net.Conn = raw
	if a.SSL {
		tc := tls.Client(raw, &tls.Config{ServerName: a.Server})
		_ = tc.SetDeadline(time.Now().Add(timeout))
		if err := tc.Handshake(); err != nil {
			raw.Close()
			if isTimeout(err) {
				return nil, fmt.Errorf("no SSL answer from %s on port %d: check the port and the SSL setting", a.Server, a.Port)
			}
			return nil, fmt.Errorf("SSL did not work with %s on port %d: check that this port uses SSL (%v)", a.Server, a.Port, err)
		}
		c = tc
	}
	conn := &Conn{Account: a, c: c, r: bufio.NewReaderSize(c, 1<<20), timeout: timeout}
	greet, err := conn.readLine()
	if err != nil {
		c.Close()
		if !a.SSL {
			return nil, fmt.Errorf("%s did not greet on port %d: if this port uses SSL, turn SSL on (%v)", a.Server, a.Port, err)
		}
		return nil, fmt.Errorf("%s did not greet on port %d: %v", a.Server, a.Port, err)
	}
	if !strings.HasPrefix(greet, "20") {
		c.Close()
		return nil, fmt.Errorf("%s turned the connection away (%s): too many connections open, or the account is not active", a.Server, greet)
	}
	if a.User != "" {
		r, err := conn.Cmd("AUTHINFO USER " + a.User)
		if err == nil && strings.HasPrefix(r, "381") {
			r, err = conn.Cmd("AUTHINFO PASS " + a.Password)
		}
		if err != nil {
			c.Close()
			return nil, err
		}
		if !strings.HasPrefix(r, "281") {
			c.Close()
			return nil, &LoginError{Server: a.Server, Reply: r}
		}
	}
	return conn, nil
}

func (c *Conn) readLine() (string, error) {
	_ = c.c.SetReadDeadline(time.Now().Add(c.timeout))
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Cmd sends one command line and returns the status line.
func (c *Conn) Cmd(cmd string) (string, error) {
	_ = c.c.SetWriteDeadline(time.Now().Add(c.timeout))
	if _, err := c.c.Write([]byte(cmd + "\r\n")); err != nil {
		return "", err
	}
	return c.readLine()
}

// Body fetches an article body, dot-unstuffed, lines kept with their CRLF.
func (c *Conn) Body(msgid string) ([]byte, error) {
	r, err := c.Cmd("BODY <" + strings.Trim(msgid, "<>") + ">")
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(r, "222"):
		return c.readBlock()
	case strings.HasPrefix(r, "430"), strings.HasPrefix(r, "423"), strings.HasPrefix(r, "420"):
		return nil, ErrMissing
	}
	return nil, fmt.Errorf("nntp: BODY <%s>: %s", msgid, r)
}

// Stat reports whether the server has the article.
func (c *Conn) Stat(msgid string) (bool, error) {
	r, err := c.Cmd("STAT <" + strings.Trim(msgid, "<>") + ">")
	if err != nil {
		return false, err
	}
	switch {
	case strings.HasPrefix(r, "223"):
		return true, nil
	case strings.HasPrefix(r, "430"), strings.HasPrefix(r, "423"), strings.HasPrefix(r, "420"):
		return false, nil
	}
	return false, fmt.Errorf("nntp: STAT: %s", r)
}

// readBlock reads a dot-terminated multi-line block.
func (c *Conn) readBlock() ([]byte, error) {
	out := make([]byte, 0, 2<<20+(2<<20)/64)
	for {
		_ = c.c.SetReadDeadline(time.Now().Add(c.timeout))
		line, err := c.r.ReadSlice('\n')
		if err == bufio.ErrBufferFull { // a line longer than the buffer: keep reading it
			out = append(out, line...)
			continue
		}
		if err != nil {
			return nil, err
		}
		if len(line) >= 2 && line[0] == '.' {
			if bytes.Equal(line, []byte(".\r\n")) || bytes.Equal(line, []byte(".\n")) {
				return out, nil
			}
			line = line[1:] // dot-stuffing
		}
		out = append(out, line...)
	}
}

// Close sends QUIT and closes the socket.
func (c *Conn) Close() {
	c.closed.Do(func() {
		runtime.SetFinalizer(c, nil)
		_ = c.c.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = c.c.Write([]byte("QUIT\r\n"))
		c.c.Close()
		if c.bud != nil {
			c.bud.release()
		}
	})
}

// IsConnErr: the connection is unusable (as opposed to a missing article).
func IsConnErr(err error) bool {
	return err != nil && !errors.Is(err, ErrMissing)
}
