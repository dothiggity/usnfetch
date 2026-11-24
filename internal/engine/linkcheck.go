package engine

import (
	"errors"
	"sync"
	"time"

	"usnfetch/internal/nntp"
)

// LinkResult says whether a share's manifest article (the first thing opening a share
// downloads) is on a Usenet account.
type LinkResult struct {
	Found bool  // some account has the manifest or one of its copies
	Err   error // nil: every account answered; set when it is unknown (no account could be asked)
}

// CheckLinks STATs each share's manifest message-ids (key: anything the caller uses to tell
// shares apart; value: the manifest's message-id and its copies) at the accounts in priority
// order: a share found at one account is not asked about at the next. It is the cheap check
// a whole imported list gets: one small request per link, no index needed. A manifest that is
// gone means the share can't be opened at all; one that is there says nothing yet about the
// files (opening the share checks a sample of those). progress, when set, is called after
// each share is settled.
func CheckLinks(manifests map[string][]string, accts *AccountSet, conns int, progress func(done int)) map[string]LinkResult {
	out := make(map[string]LinkResult, len(manifests))
	open := make([]string, 0, len(manifests)) // not found anywhere yet
	for k := range manifests {
		open = append(open, k)
	}
	errs := map[string]error{}   // the last error each open share met
	answered := map[string]int{} // accounts that answered for it
	done := 0
	var mu sync.Mutex
	for _, a := range accts.All() {
		if len(open) == 0 {
			break
		}
		next := 0
		var wg sync.WaitGroup
		for w := 0; w < max(1, min(conns, a.Connections, len(open))); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c, err := nntp.Dial(a, 30*time.Second)
				if err != nil {
					mu.Lock()
					for _, k := range open { // this worker's share of the work falls to the others
						if _, ok := errs[k]; !ok {
							errs[k] = err
						}
					}
					mu.Unlock()
					return
				}
				defer c.Close()
				for {
					mu.Lock()
					if next >= len(open) {
						mu.Unlock()
						return
					}
					k := open[next]
					next++
					mu.Unlock()
					found, err := false, error(nil)
					for _, m := range manifests[k] {
						if found, err = c.Stat(m); found || err != nil {
							break
						}
					}
					mu.Lock()
					if err != nil {
						errs[k] = err
						mu.Unlock()
						return // a connection that failed once is not trusted with the rest
					}
					answered[k]++
					delete(errs, k)
					if found {
						out[k] = LinkResult{Found: true}
						done++
						if progress != nil {
							progress(done)
						}
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		still := open[:0]
		for _, k := range open {
			if !out[k].Found {
				still = append(still, k)
			}
		}
		open = still
	}
	for _, k := range open {
		if answered[k] > 0 && errs[k] == nil {
			out[k] = LinkResult{} // every account that could be asked said it isn't there
		} else {
			err := errs[k]
			if err == nil {
				err = errNoAnswer
			}
			out[k] = LinkResult{Err: err}
		}
		done++
		if progress != nil {
			progress(done)
		}
	}
	return out
}

var errNoAnswer = errors.New("no Usenet account could be asked")
