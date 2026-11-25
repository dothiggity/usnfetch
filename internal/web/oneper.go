package web

import (
	"sort"
	"strings"

	"usnfetch/internal/share"
)

// One copy per game (share.OnePerGame): a selection that takes whole system folders can be
// narrowed to each game's best release, by the regions Settings prefers.

// systemsIn: the build's system folders that a selection takes whole.
func systemsIn(ix *share.Index, sel *selection) ([]share.System, error) {
	systems, err := ix.Inside()
	if err != nil {
		return nil, err
	}
	var out []share.System
	for _, sy := range systems {
		for _, f := range sel.folders {
			if f == "" || sy.Folder == f || strings.HasPrefix(sy.Folder, f+"/") {
				out = append(out, sy)
				break
			}
		}
	}
	return out, nil
}

// onePerGame narrows sel: each system folder it takes whole becomes the files of each game's
// best release; the rest of it (other folders, single files) stays as it was. games: how many
// games, left: the files left out.
func (s *Server) onePerGame(ix *share.Index, sel *selection) (out *selection, games, left int, err error) {
	systems, err := systemsIn(ix, sel)
	if err != nil || len(systems) == 0 {
		return sel, 0, 0, err
	}
	prefer := s.M.Settings.Regions
	out = &selection{}
	exact := map[string]string{} // with / -> as the share names it
	var prefixes []string
	covered := map[string]bool{} // system folders now taken game by game
	for _, sy := range systems {
		keep, l, err := ix.OnePerGame(sy.Folder, prefer)
		if err != nil {
			return nil, 0, 0, err
		}
		covered[sy.Folder] = true
		left += l
		seen := map[string]bool{}
		for _, g := range keep {
			if k := strings.ToLower(share.TitleKey(share.GameTitle(g.Name))); !seen[k] {
				seen[k] = true
				games++
			}
			if g.Folder {
				prefixes = append(prefixes, slash(g.Path)+"/")
				out.folders = append(out.folders, slash(g.Path))
				out.count += g.Count
			} else {
				exact[slash(g.Path)] = g.Path
				out.count++
			}
			out.size += g.Size
		}
	}
	// what else was selected: folders that aren't (or don't hold) a system folder taken above
	for _, f := range sel.folders {
		if f == "" || covered[f] {
			continue
		}
		holds := false
		for c := range covered {
			if strings.HasPrefix(c, f+"/") {
				holds = true
			}
		}
		if holds {
			continue // e.g. "roms": its systems are taken game by game; its other files stay out
		}
		if d, _ := ix.Dir(f); d != nil {
			prefixes = append(prefixes, d.Path+"/")
			out.folders = append(out.folders, d.Path)
			out.count += d.Count
			out.size += d.Size
		}
	}
	for _, p := range sel.exact {
		if _, have := exact[slash(p)]; !have {
			exact[slash(p)] = p
			if fe, _ := ix.FileByPath(p); fe != nil {
				out.count++
				out.size += fe.Size
			}
		}
	}
	for _, p := range exact {
		out.exact = append(out.exact, p)
	}
	sort.Strings(out.exact)
	sort.Strings(prefixes)
	sort.Strings(out.folders)
	out.sel = append(append([]string{}, out.exact...), prefixes...)
	if out.count == 0 {
		return nil, 0, 0, fail(400, "no games in that", "empty")
	}
	return out, games, left, nil
}
