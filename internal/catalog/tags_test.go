package catalog

import (
	"slices"
	"testing"
)

func TestTags(t *testing.T) {
	cases := []struct {
		title, variant string
		also           []string
		launchers      []string
		devices        []string
	}{
		// made-up titles, each worded to hit one rule
		{"Test set 2TB Batocera for a Raspberry Pi 4 or P400", "", nil, []string{"Batocera"}, []string{"Raspberry Pi 4"}},
		{"Test set 1TB Batocera for a Raspberry Pi 5", "", nil, []string{"Batocera"}, []string{"Raspberry Pi 5"}},
		{"Test set 1TB Batocera for a Windows desktop / laptop", "", nil, []string{"Batocera"}, []string{"PC"}},
		// the label's tags
		{"Sample v42 – 256GB Batocera Build", "256GB-SampleV42-PC-STEAMDECK-XBOXALLYX-Batocera", nil,
			[]string{"Batocera"}, []string{"Steam Deck", "ROG Ally", "PC"}},
		{"Test image 128gb Pi4 & Pi3 Batocera 36", "128GB-TestImage-RPI4-RPI3-Batocera", nil, []string{"Batocera"}, []string{"Raspberry Pi 4", "Raspberry Pi 3"}},
		// other names the same link was listed under count too
		{"Test set 1TB Retrobat for ROG ALLY, ROG ALLY X and AYAYNEO", "", []string{"Test set 1TB RetroBat for MSI Claw"},
			[]string{"RetroBat"}, []string{"ROG Ally", "MSI Claw", "AYANEO"}},
		// Windows-only launchers are for a PC when nothing else is named ...
		{"Test set 8TB Hyperspin", "", nil, []string{"HyperSpin"}, []string{"PC"}},
		{"Test set 20TB LaunchBox", "", nil, []string{"LaunchBox"}, []string{"PC"}},
		// ... but a named device wins
		{"Test image 16gb Pi4 cabinet", "16GB-TestImagePi4Cabinet-RPI4-HyperSpin", nil, []string{"HyperSpin"}, []string{"Raspberry Pi 4"}},
		{"Test set 1TB Teknoparrot with Retrobat", "", nil, []string{"RetroBat", "TeknoParrot"}, []string{"PC"}},
		// a size is not a model number; a Pi without a model is just a Raspberry Pi
		{"128gb Raspberry Pi image", "", nil, []string{}, []string{"Raspberry Pi"}},
		{"Test image 32gb for a GPI Case", "", nil, []string{}, []string{"Raspberry Pi"}},
		{"Test image 16gb Raspberry Pi for the Rpi2-3b", "16GB-TestImageRaspberryPi-RPI3-RetroPie", nil, []string{"RetroPie"}, []string{"Raspberry Pi 3", "Raspberry Pi 2"}},
		{"Test image PiZero 32gb RETROPIE for the PI 0/PI 0 W/ Pi 1", "", nil, []string{"RetroPie"}, []string{"Raspberry Pi Zero", "Raspberry Pi 1"}},
		{"Test image Pi Zero 2 W GPI Case RecalBox", "", nil, []string{"Recalbox"}, []string{"Raspberry Pi Zero"}},
		// words that are not what they look like
		{"[128GB] BATOCERA 5.20 RPI 3b+ test image, PSP VIDEOS + KODI No Recalbox", "", nil, []string{"Batocera"}, []string{"Raspberry Pi 3"}},
		{"Test image 16gb Raspberry Pi 4 LightGun WiiMote Ready", "", nil, []string{}, []string{"Raspberry Pi 4"}},
		{"PC Engine Collection", "", nil, []string{}, []string{}},
		{"STEAMY DECK build", "", nil, []string{}, []string{"Steam Deck"}},
		{"2TB XBOX360-RGH", "", nil, []string{}, []string{"Xbox 360"}},
		{"XBOX test collection [CCI]", "", nil, []string{}, []string{"Xbox"}},
		{"Test set 1.5TB for Retroid Pocket 4 Pro RP5, Odin, Anbernic RG556 Cube Mini and Android Handhelds", "", nil,
			[]string{"Android"}, []string{"Retroid Pocket", "Anbernic", "AYN Odin"}},
		{"Test image version 2 128gb Odroid Go Batocera", "", nil, []string{"Batocera"}, []string{"Odroid Go"}},
		{"Test image 128gb Batocera for Odroid N2 or S905x", "", nil, []string{"Batocera"}, []string{"Odroid", "Amlogic TV box"}},
		{"Playstation Classic test build 128gb", "128GB-SampleBuild-PLAYSTATIONCLASSIC-EmulationStation", nil,
			[]string{"EmulationStation"}, []string{"PlayStation Classic"}},
		{"Test set 1TB XSTATION PS1 NTSC", "", nil, []string{}, []string{"PlayStation (XStation)"}},
		{"Test set MAME 0.258 + CHDs", "", nil, []string{}, []string{}},
	}
	for _, c := range cases {
		e := Entry{Title: c.title, Variant: c.variant, Also: c.also}
		l, d := Tags(e)
		if !slices.Equal(l, c.launchers) || !slices.Equal(d, c.devices) {
			t.Errorf("%q / %q:\n  launchers %q, want %q\n  devices   %q, want %q", c.title, c.variant, l, c.launchers, d, c.devices)
		}
	}
	// the opened share's own name is read as well
	if _, d := Tags(Entry{Title: "Some build"}, "128GB-PSP-CONSOLE.img"); !slices.Equal(d, []string{"PSP"}) {
		t.Errorf("share name: devices %q", d)
	}
}

func TestDeviceGroups(t *testing.T) {
	for _, r := range deviceRules {
		if r.tag != "" && !slices.Contains(DeviceGroups, r.group) {
			t.Errorf("device %q: group %q is not in DeviceGroups", r.tag, r.group)
		}
	}
	if DeviceGroup("Steam Deck") != "Handheld PCs" || DeviceGroup("Raspberry Pi 5") != "Raspberry Pi" || DeviceGroup("nope") != "" {
		t.Error("DeviceGroup")
	}
}
