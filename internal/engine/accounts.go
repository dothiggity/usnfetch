// Package engine downloads share files: segmented files written once at their final
// offsets with a resumable checkpoint, packed small files, availability checks and
// on-demand range reads, over one or more Usenet accounts.
package engine

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"usnfetch/internal/nntp"
	"usnfetch/internal/share"
	"usnfetch/internal/yenc"
)

// AccountSet orders the accounts by priority, as SABnzbd does with its servers: the
// accounts with the best (lowest) priority number share the downloading, each up to its own
// connection count; the others are asked, best priority first, only for articles that are
// missing or corrupt on every copy at the accounts above them.
type AccountSet struct {
	Primary []*nntp.Account // the top priority group
	Backup  []*nntp.Account // everything else, by priority
}

func NewAccountSet(accts []*nntp.Account) (*AccountSet, error) {
	if len(accts) == 0 {
		return nil, errors.New("no Usenet account configured (Settings)")
	}
	sorted := append([]*nntp.Account{}, accts...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority < sorted[j].Priority })
	s := &AccountSet{}
	for _, a := range sorted {
		a.Backup = a.Priority > sorted[0].Priority
		if a.Backup {
			s.Backup = append(s.Backup, a)
		} else {
			s.Primary = append(s.Primary, a)
		}
	}
	return s, nil
}

func (s *AccountSet) All() []*nntp.Account {
	return append(append([]*nntp.Account{}, s.Primary...), s.Backup...)
}

func (s *AccountSet) Connections() int {
	n := 0
	for _, a := range s.Primary {
		n += max(1, a.Connections)
	}
	return n
}

// Slots: n worker slots spread over the primaries in proportion to their connection counts.
func (s *AccountSet) Slots(n int) []*nntp.Account {
	left := map[*nntp.Account]int{}
	for _, a := range s.Primary {
		left[a] = max(1, a.Connections)
	}
	var out []*nntp.Account
	for len(out) < n {
		added := false
		for _, a := range s.Primary {
			if left[a] > 0 && len(out) < n {
				out = append(out, a)
				left[a]--
				added = true
			}
		}
		if !added {
			break
		}
	}
	if len(out) == 0 {
		out = s.Primary[:1]
	}
	return out
}

func (s *AccountSet) Describe() string {
	var p []string
	for _, a := range s.Primary {
		p = append(p, fmt.Sprintf("%s x%d", a.Label(), a.Connections))
	}
	out := strings.Join(p, ", ")
	if len(s.Backup) > 0 {
		var b []string
		for _, a := range s.Backup {
			b = append(b, fmt.Sprintf("%s (priority %d)", a.Label(), a.Priority))
		}
		out += "; if those lack an article: " + strings.Join(b, ", ")
	}
	return out
}

// DialTimeout for article connections.
var DialTimeout = 60 * time.Second

// Pool keeps idle connections per account between files and jobs.
type Pool struct {
	Accounts *AccountSet
	Bulk     bool // a download's pool: its connections give way to other uses (nntp budget)
	mu       sync.Mutex
	idle     map[*nntp.Account][]idleConn
	backup   map[*nntp.Account]chan struct{}
}

type idleConn struct {
	c     *nntp.Conn
	since time.Time
}

// IdleMax: a connection idle longer than this is not reused. Servers drop idle
// connections without a word, and the first write on one then fails.
var IdleMax = 50 * time.Second

func NewPool(s *AccountSet) *Pool {
	p := &Pool{Accounts: s, idle: map[*nntp.Account][]idleConn{}, backup: map[*nntp.Account]chan struct{}{}}
	for _, a := range s.Backup {
		p.backup[a] = make(chan struct{}, max(1, a.Connections))
	}
	return p
}

func (p *Pool) Get(a *nntp.Account) (*nntp.Conn, error) {
	if a == nil {
		a = p.Accounts.Primary[0]
	}
	p.mu.Lock()
	if l := p.idle[a]; len(l) > 0 {
		ic := l[len(l)-1] // the most recently used
		if time.Since(ic.since) <= IdleMax {
			p.idle[a] = l[:len(l)-1]
			p.mu.Unlock()
			return ic.c, nil
		}
		p.idle[a] = nil // this one and every older one have sat too long
		p.mu.Unlock()
		for _, x := range l {
			x.c.Close()
		}
		return p.dial(a)
	}
	p.mu.Unlock()
	return p.dial(a)
}

func (p *Pool) dial(a *nntp.Account) (*nntp.Conn, error) {
	if p.Bulk {
		return nntp.DialBulk(a, DialTimeout)
	}
	return nntp.Dial(a, DialTimeout)
}

func (p *Pool) Put(c *nntp.Conn) {
	if c.Yield() { // something else is waiting for a connection: hand this one back
		c.Close()
		return
	}
	p.mu.Lock()
	p.idle[c.Account] = append(p.idle[c.Account], idleConn{c, time.Now()})
	p.mu.Unlock()
}

// DropIdle closes an account's idle connections (after one of them turned out dead).
func (p *Pool) DropIdle(a *nntp.Account) {
	p.mu.Lock()
	l := p.idle[a]
	p.idle[a] = nil
	p.mu.Unlock()
	for _, x := range l {
		x.c.Close()
	}
}

func (p *Pool) Close() {
	p.mu.Lock()
	idle := p.idle
	p.idle = map[*nntp.Account][]idleConn{}
	p.mu.Unlock()
	for _, l := range idle {
		for _, x := range l {
			x.c.Close()
		}
	}
}

// ErrCorrupt: every copy of the article that exists failed its checks.
var ErrCorrupt = errors.New("article corrupt on every copy")

// NewAEAD for a share's folder key.
func NewAEAD(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// FetchAndDecrypt fetches one segment's article and AES-GCM-opens it: primary first,
// then the redundancy copies. A missing article, a yEnc/size error or a GCM tag
// failure on one copy falls through to the next. Errors: nntp.ErrMissing when every
// copy is absent, ErrCorrupt when the ones present are damaged, else a connection error.
func FetchAndDecrypt(c *nntp.Conn, seg share.Segment, aead cipher.AEAD, checkSize bool) ([]byte, error) {
	var last error = nntp.ErrMissing
	for _, m := range seg.Msgids {
		body, err := c.Body(m)
		if errors.Is(err, nntp.ErrMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		ct, _, err := yenc.Decode(body)
		if err != nil {
			last = fmt.Errorf("%w: <%s>: %v", ErrCorrupt, m, err)
			continue
		}
		if checkSize && int64(len(ct)) != seg.Size {
			last = fmt.Errorf("%w: ciphertext %d B, expected %d B from <%s>", ErrCorrupt, len(ct), seg.Size, m)
			continue
		}
		pt, err := aead.Open(ct[:0], seg.Nonce, append(ct, seg.Tag...), nil)
		if err != nil {
			last = fmt.Errorf("%w: AES-GCM tag failure on <%s>", ErrCorrupt, m)
			continue
		}
		return pt, nil
	}
	return nil, last
}

// IsArticleErr: the article is unusable (missing/corrupt), as opposed to the connection.
func IsArticleErr(err error) bool {
	return errors.Is(err, nntp.ErrMissing) || errors.Is(err, ErrCorrupt)
}

// BackupFetch asks the backup accounts for a segment the primaries could not supply.
func (p *Pool) BackupFetch(seg share.Segment, aead cipher.AEAD, checkSize bool) []byte {
	for _, a := range p.Accounts.Backup {
		sem := p.backup[a]
		sem <- struct{}{}
		data := func() []byte {
			defer func() { <-sem }()
			c, err := p.Get(a)
			if err != nil {
				return nil
			}
			data, err := FetchAndDecrypt(c, seg, aead, checkSize)
			if err != nil && !IsArticleErr(err) {
				c.Close()
				return nil
			}
			p.Put(c)
			return data
		}()
		if data != nil {
			return data
		}
	}
	return nil
}
