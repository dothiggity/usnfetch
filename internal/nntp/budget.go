package nntp

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Connection budget: the connections this process holds to one provider account at once,
// never more than the account's Connections setting (what the provider allows: Easynews, for
// one, allows 60 and turns the 61st away). Everything shares it: parallel downloads,
// streaming, availability checks, link checks and index fetches.
//
// Downloads dial with DialBulk and may take the whole budget. When anything else is waiting
// for a connection, downloads give way: no new bulk connection is handed out, and a download
// worker hands its connection back after the article it is on (Conn.Yield), so a preview or a
// check gets its connection within about an article's time, and the download takes it back
// afterwards.

// ErrBusy: every connection the provider allows is in use right now.
var ErrBusy = errors.New("every connection the provider allows is in use")

type budget struct {
	mu      sync.Mutex
	cond    *sync.Cond
	limit   int
	used    int
	waiting int // non-download dials waiting for a connection
}

var (
	budgetsMu sync.Mutex
	budgets   = map[string]*budget{}
)

// budgetOf: the account's budget (one per server, port and username), following its
// current Connections setting.
func budgetOf(a *Account) *budget {
	key := fmt.Sprintf("%s|%d|%s", strings.ToLower(a.Server), a.Port, a.User)
	budgetsMu.Lock()
	b := budgets[key]
	if b == nil {
		b = &budget{}
		b.cond = sync.NewCond(&b.mu)
		budgets[key] = b
	}
	budgetsMu.Unlock()
	b.mu.Lock()
	changed := b.limit != max(1, a.Connections)
	b.limit = max(1, a.Connections)
	b.mu.Unlock()
	if changed {
		b.cond.Broadcast()
	}
	return b
}

// acquire takes a place, waiting up to wait. A download (bulk) also waits while anything
// else is waiting.
func (b *budget) acquire(bulk bool, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	wake := time.AfterFunc(wait, b.cond.Broadcast)
	defer wake.Stop()
	b.mu.Lock()
	defer b.mu.Unlock()
	if !bulk {
		b.waiting++
		defer func() { b.waiting-- }()
	}
	for b.used >= b.limit || (bulk && b.waiting > 0) {
		if !time.Now().Before(deadline) {
			return false
		}
		b.cond.Wait()
	}
	b.used++
	return true
}

func (b *budget) release() {
	b.mu.Lock()
	b.used--
	b.mu.Unlock()
	b.cond.Broadcast()
}

func (b *budget) wanted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.waiting > 0
}

// Waits for a place in the budget: other uses wait longer (downloads give way within about an
// article's time); a download's worker retries after its wait.
var (
	BulkWait  = 10 * time.Second
	OtherWait = 45 * time.Second
)

// InUse reports the connections held to an account and its budget (System page, tests).
func InUse(a *Account) (used, limit int) {
	b := budgetOf(a)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used, b.limit
}
