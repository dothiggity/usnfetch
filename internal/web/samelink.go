package web

import (
	"slices"

	"usnfetch/internal/share"
)

// One share, written two ways. A pasted link can be the share you already have under
// another spelling: usenet:// instead of usn://, or cut off at the end by a forum's line
// wrap. Taken at its word it's a new share, and downloading it fetches the whole build again
// into a folder of its own. sameShare finds the link usnfetch already has for it instead.

// knownLinks: every link usnfetch has (the catalog, the jobs, shares opened by their link),
// by share.LinkKey.
func (s *Server) knownLinks() map[string][]string {
	out := map[string][]string{}
	seen := map[string]bool{}
	add := func(l string) {
		if seen[l] {
			return
		}
		seen[l] = true
		if k := share.LinkKey(l); k != "" {
			out[k] = append(out[k], l)
		}
	}
	for _, e := range s.Cat.Snapshot() {
		add(e.Link)
	}
	for _, j := range s.M.Queue() {
		add(j.Link)
	}
	for _, j := range s.M.History() {
		add(j.Link)
	}
	for _, l := range s.M.Store.ShareLinks() {
		add(l)
	}
	return out
}

// linkBits: what a link says beyond its share: the publisher key it pins, the folder key
// after '#' on old links, and how many copies of the manifest it names. Zero for a link cut
// too short to read.
type linkBits struct {
	pin, key string
	copies   int
}

func bitsOf(link string) linkBits {
	if l, err := share.ParseLink(link); err == nil {
		return linkBits{l.OwnerFP, l.KeyHex, len(l.ManifestMsgids)}
	}
	return linkBits{}
}

// covers: the known link k says everything the pasted link n does, so swapping n for it
// loses nothing.
func covers(k, n linkBits) bool {
	return (n.pin == "" || n.pin == k.pin) && (n.key == "" || n.key == k.key) && n.copies <= k.copies
}

// sameShare: the link usnfetch already has for the share link names, else link itself.
// known is knownLinks(), nil to work it out here. A link is only swapped for one that says
// everything it does (covers): one pinning the publisher's key, carrying the folder key or
// naming more manifest copies is kept as it is.
func (s *Server) sameShare(link string, known map[string][]string) string {
	key := share.LinkKey(link)
	if key == "" {
		return link
	}
	if known == nil {
		if _, ok := s.Cat.ByLink(link); ok {
			return link // the catalog's own link
		}
		known = s.knownLinks()
	}
	have := known[key]
	for _, k := range have {
		if k == link {
			return link
		}
	}
	n := bitsOf(link)
	for _, k := range have {
		if covers(bitsOf(k), n) {
			return k
		}
	}
	return link
}

// sameAs: sameShare for many links, working out the known ones once. Each new link joins
// them, so the same share twice in one paste or list is one share.
func (s *Server) sameAs() func(string) string {
	known := s.knownLinks()
	return func(l string) string {
		got := s.sameShare(l, known)
		if k := share.LinkKey(got); got == l && k != "" && !slices.Contains(known[k], got) {
			known[k] = append(known[k], got)
		}
		return got
	}
}
