package engine

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"usnfetch/internal/nntp"
	"usnfetch/internal/share"
)

// Sampled is one sampled segment and what it belongs to.
type Sampled struct {
	Label string
	Seg   share.Segment
}

// SampleSegments: up to n segments spread evenly over every file's segments and every
// packed segment.
func SampleSegments(ix *share.Index, n int) ([]Sampled, error) {
	totalSegs, err := ix.TotalSegments()
	if err != nil {
		return nil, err
	}
	pids, err := ix.PackIDs()
	if err != nil {
		return nil, err
	}
	total := totalSegs + int64(len(pids))
	if total == 0 || n <= 0 {
		return nil, nil
	}
	n = int(min(int64(n), total))
	set := map[int64]bool{}
	for k := 0; k < n; k++ {
		set[int64(float64(k)*float64(total)/float64(n))] = true
	}
	if n > 1 {
		set[total-1] = true
	}
	picks := make([]int64, 0, len(set))
	for p := range set {
		picks = append(picks, p)
	}
	sort.Slice(picks, func(i, j int) bool { return picks[i] < picks[j] })
	type hit struct {
		fe share.FileEntry
		i  int64
	}
	var chosen []hit
	pi, base := 0, int64(0)
	_ = ix.IterSegmentedFiles(func(fe share.FileEntry) bool {
		end := base + fe.SegCount
		for pi < len(picks) && picks[pi] < end {
			chosen = append(chosen, hit{fe, picks[pi] - base})
			pi++
		}
		base = end
		return pi < len(picks) && picks[pi] < totalSegs
	})
	out := make([]Sampled, 0, len(picks))
	plans := map[int64]*share.Plan{}
	for _, h := range chosen {
		pl := plans[h.fe.FID]
		if pl == nil {
			if pl, err = ix.Plan(h.fe); err != nil {
				return nil, err
			}
			plans[h.fe.FID] = pl
		}
		seg, err := ix.Segment(pl, h.i)
		if err != nil {
			return nil, err
		}
		out = append(out, Sampled{h.fe.Path, seg})
	}
	for _, p := range picks[pi:] {
		pid := pids[p-totalSegs]
		seg, err := ix.PackSegment(pid)
		if err != nil {
			return nil, err
		}
		out = append(out, Sampled{fmt.Sprintf("packed segment %d", pid), seg})
	}
	return out, nil
}

// AccountCheck is one account's availability result.
type AccountCheck struct {
	Account   string   `json:"account"`
	Backup    bool     `json:"backup"`
	Priority  int      `json:"priority"`
	Checked   int      `json:"checked"`
	PrimaryOK int      `json:"primary_ok"`
	Available int      `json:"available"`
	Missing   []string `json:"missing"`
	Error     *string  `json:"error"`
}

// Availability is a check result.
type Availability struct {
	Sampled   int            `json:"sampled"`
	Available int            `json:"available"`
	CheckedAt float64        `json:"checked_at"`
	Verdict   string         `json:"verdict"`
	Accounts  []AccountCheck `json:"accounts"`
	Error     string         `json:"error,omitempty"`
}

// CheckAvailability STATs a spread sample of the share's articles at every account. A
// segment is available at an account when its primary article or any redundancy copy is
// there; the share is "available" when every sampled segment is available somewhere.
func CheckAvailability(ix *share.Index, accts *AccountSet, sample, conns int) (*Availability, error) {
	segs, err := SampleSegments(ix, sample)
	if err != nil {
		return nil, err
	}
	res := &Availability{Sampled: len(segs), CheckedAt: float64(time.Now().UnixNano()) / 1e9, Accounts: []AccountCheck{}}
	anywhere := map[int]bool{}
	for _, a := range accts.All() {
		ac := AccountCheck{Account: a.Label(), Backup: a.Backup, Priority: a.Priority, Missing: []string{}}
		var mu sync.Mutex
		ok := map[int]bool{}
		next := 0
		var wg sync.WaitGroup
		for w := 0; w < max(1, min(conns, a.Connections, len(segs))); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c, err := nntp.Dial(a, 30*time.Second)
				if err != nil {
					mu.Lock()
					s := err.Error()
					ac.Error = &s
					mu.Unlock()
					return
				}
				defer c.Close()
				for {
					mu.Lock()
					if next >= len(segs) {
						mu.Unlock()
						return
					}
					k := next
					next++
					mu.Unlock()
					s := segs[k]
					prim, err := c.Stat(s.Seg.Msgids[0])
					found := prim
					for _, m := range s.Seg.Msgids[1:] {
						if found || err != nil {
							break
						}
						found, err = c.Stat(m)
					}
					mu.Lock()
					if err != nil {
						e := err.Error()
						ac.Error = &e
						mu.Unlock()
						return
					}
					ac.Checked++
					if prim {
						ac.PrimaryOK++
					}
					if found {
						ac.Available++
						ok[k] = true
					} else if len(ac.Missing) < 20 {
						ac.Missing = append(ac.Missing, s.Label)
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		for k := range ok {
			anywhere[k] = true
		}
		res.Accounts = append(res.Accounts, ac)
	}
	res.Available = len(anywhere)
	allErr, complete := true, true
	for _, a := range res.Accounts {
		if a.Error == nil {
			allErr = false
			if a.Checked != len(segs) {
				complete = false
			}
		}
	}
	switch {
	case len(segs) == 0:
		res.Verdict = "empty"
	case allErr:
		res.Verdict = "error"
	case len(anywhere) == len(segs):
		res.Verdict = "available"
	case len(anywhere) > 0:
		res.Verdict = "partial"
	case complete:
		res.Verdict = "missing"
	default:
		res.Verdict = "error"
	}
	return res, nil
}
