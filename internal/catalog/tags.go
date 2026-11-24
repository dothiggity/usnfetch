package catalog

import (
	"regexp"
	"slices"
	"strings"
)

// Launchers and devices: what a build runs (Batocera, RetroBat, LaunchBox…) and what it is
// for (a PC, a Steam Deck, a Raspberry Pi 5…), read from the words of its listing. Lists
// write these many ways ("Raspberry Pi 5", "RPi5", "-RPI5-"; "ROG ALLY", "XBOXALLYX"), and
// the rules below give each one name, so the catalog can filter and sort by them.

// tagRule: text a rule recognises and the name it gives. Rules run in order and each takes
// the words it matched out of the text, so a later rule never reads them again ("Xbox Ally"
// is a ROG Ally, not an Xbox). A rule without a tag only takes words out ("no Recalbox").
type tagRule struct {
	re       *regexp.Regexp
	tag      string
	group    string // devices: the heading the device is listed under
	fallback bool   // given only when no other device of its group was found
}

func rule(re, tag, group string) tagRule {
	return tagRule{re: regexp.MustCompile(`(?i)` + re), tag: tag, group: group}
}

func fallback(re, tag, group string) tagRule {
	r := rule(re, tag, group)
	r.fallback = true
	return r
}

// piModel: a Raspberry Pi model number however it is written ("Raspberry Pi 4", "RPi4",
// "Pi4", "RaspberryPi4Image"), not followed by another digit ("Pi 128GB" is not a Pi 1).
func piModel(n string) string {
	return `(?:raspberry\s*pi|\brpi|\bpi)\s*` + n + `(?:[^0-9]|$)`
}

// Device groups, in the order the device filter lists them.
const (
	groupPC       = "PC"
	groupHandheld = "Handheld PCs"
	groupPi       = "Raspberry Pi"
	groupRetro    = "Retro handhelds"
	groupBoards   = "Other boards"
	groupConsoles = "Consoles"
)

// DeviceGroups: the device groups, in the order they are listed.
var DeviceGroups = []string{groupPC, groupHandheld, groupPi, groupRetro, groupBoards, groupConsoles}

var deviceRules = []tagRule{
	rule(`pc[\s-]*engine`, "", ""), // a console's name, not a PC
	rule(`playstation\s*classic`, "PlayStation Classic", groupConsoles),

	rule(`steamy?\s*deck`, "Steam Deck", groupHandheld), // "STEAMY DECK" too
	rule(`(?:rog\s*)?xbox\s*ally(?:\s*x)?|rog\s*ally(?:\s*x)?`, "ROG Ally", groupHandheld),
	rule(`legion\s*go`, "Legion Go", groupHandheld),
	rule(`msi\s*claw`, "MSI Claw", groupHandheld),
	rule(`aya\s*y?neo`, "AYANEO", groupHandheld),

	rule(piModel("5")+`|\bp500\b`, "Raspberry Pi 5", groupPi),
	rule(piModel("4")+`|(?:raspberry\s*pi|\bpi)\s*400\b|\bp400\b|nespi\s*4`, "Raspberry Pi 4", groupPi),
	rule(piModel("3"), "Raspberry Pi 3", groupPi),
	rule(piModel("2"), "Raspberry Pi 2", groupPi),
	rule(`(?:raspberry\s*pi|\brpi|\bpi)\s*(?:zero|0(?:[^0-9]|$))`, "Raspberry Pi Zero", groupPi),
	rule(piModel("1"), "Raspberry Pi 1", groupPi),
	fallback(`raspberry\s*pi|\brpi\b|\bgpi|\bpiboy|\bnespi`, "Raspberry Pi", groupPi),

	rule(`retroid\s*pocket`, "Retroid Pocket", groupRetro),
	rule(`anbernic|\brg\s*\d{2,3}`, "Anbernic", groupRetro),
	rule(`\bodin\b`, "AYN Odin", groupRetro),
	rule(`odroid[\s-]*go`, "Odroid Go", groupRetro),
	rule(`pocket\s*go`, "Pocket Go", groupRetro),

	rule(`odroid`, "Odroid", groupBoards),
	rule(`\bs905`, "Amlogic TV box", groupBoards),

	rule(`\bpc\b|windows|desktop|laptop`, "PC", groupPC),

	rule(`xbox\s*360`, "Xbox 360", groupConsoles),
	rule(`\bxbox\b`, "Xbox", groupConsoles),
	rule(`\bps\s*4\b|playstation\s*4`, "PS4", groupConsoles),
	rule(`\bps\s*3\b|playstation\s*3`, "PS3", groupConsoles),
	rule(`wii\s*u\b`, "Wii U", groupConsoles),
	rule(`\bwii\b`, "Wii", groupConsoles),
	rule(`xstation`, "PlayStation (XStation)", groupConsoles),
	rule(`gdemu`, "Dreamcast (GDEMU)", groupConsoles),
	rule(`psp[\s-]*console`, "PSP", groupConsoles),
	rule(`arcade\s*1\s*up`, "Arcade1Up", groupConsoles),
}

var launcherRules = []tagRule{
	rule(`\bno\s+recal\s*box`, "", ""), // "… KODI No Recalbox"
	rule(`batocera`, "Batocera", ""),
	rule(`\bretro\s*bat\b`, "RetroBat", ""),
	rule(`launch\s*box`, "LaunchBox", ""),
	rule(`hyper\s*spin`, "HyperSpin", ""),
	rule(`coin\s*ops`, "CoinOPS", ""),
	rule(`retro\s*pie`, "RetroPie", ""),
	rule(`recal\s*box`, "Recalbox", ""),
	rule(`knulli`, "KNULLI", ""),
	rule(`emu\s*elec`, "EmuELEC", ""),
	rule(`retro\s*max`, "RetroMAX", ""),
	rule(`tekno\s*parrot`, "TeknoParrot", ""),
	rule(`virtual\s*pinball|\bvpx\b|pinup\s*popper`, "Virtual Pinball", ""),
	rule(`emulation\s*station|\bes-de\b`, "EmulationStation", ""),
	rule(`\bandroid\b`, "Android", ""),
	rule(`emu\s*deck`, "EmuDeck", ""),
	rule(`retro\s*deck`, "RetroDECK", ""),
	rule(`playnite`, "Playnite", ""),
	rule(`\blakka\b`, "Lakka", ""),
	rule(`\barkos\b`, "ArkOS", ""),
	rule(`rocknix`, "ROCKNIX", ""),
	rule(`\bjelos\b`, "JELOS", ""),
	rule(`\bmuos\b`, "muOS", ""),
}

// deviceGroup: each device name's group.
var deviceGroup = func() map[string]string {
	m := map[string]string{}
	for _, r := range deviceRules {
		if r.tag != "" {
			m[r.tag] = r.group
		}
	}
	return m
}()

// DeviceNames: every device name, in the order of the rules (the order the filter lists
// them in, within their group).
var DeviceNames = func() []string {
	var out []string
	for _, r := range deviceRules {
		if r.tag != "" && !slices.Contains(out, r.tag) {
			out = append(out, r.tag)
		}
	}
	return out
}()

// DeviceGroup is the group a device name is listed under ("" for an unknown name).
func DeviceGroup(device string) string { return deviceGroup[device] }

// applyRules gives the tags of rules matching text, in rule order, each once.
func applyRules(text string, rules []tagRule) []string {
	tags := []string{}
	var later []tagRule
	for _, r := range rules {
		if r.fallback {
			later = append(later, r)
			continue
		}
		found := false
		text = r.re.ReplaceAllStringFunc(text, func(m string) string {
			found = true
			return strings.Repeat(" ", len(m))
		})
		if found && r.tag != "" && !slices.Contains(tags, r.tag) {
			tags = append(tags, r.tag)
		}
	}
	for _, r := range later {
		has := slices.ContainsFunc(tags, func(t string) bool { return deviceGroup[t] == r.group })
		if !has && r.re.MatchString(text) {
			tags = append(tags, r.tag)
		}
	}
	return tags
}

// tagText: everything a listing says about an entry, plus extra names (such as the
// opened share's own name). Parts are kept apart so a rule never reads across two.
func tagText(e Entry, extra []string) string {
	parts := append(append([]string{e.Title, e.Variant}, e.Also...), extra...)
	return strings.Join(parts, " | ")
}

// windowsOnly: launchers that run on Windows alone, so a build of one that names no device
// is for a PC ("8TB Hyperspin Download"). Lists name some Pi images HyperSpin too: a named
// device always wins.
var windowsOnly = []string{"RetroBat", "LaunchBox", "HyperSpin", "TeknoParrot", "RetroMAX", "Playnite"}

// Tags: the launchers and the devices an entry names, each in the order of the rules
// above. extra: more names to read, such as the opened share's own.
func Tags(e Entry, extra ...string) (launchers, devices []string) {
	text := tagText(e, extra)
	launchers, devices = applyRules(text, launcherRules), applyRules(text, deviceRules)
	if len(devices) == 0 && slices.ContainsFunc(launchers, func(l string) bool { return slices.Contains(windowsOnly, l) }) {
		devices = append(devices, "PC")
	}
	return launchers, devices
}
