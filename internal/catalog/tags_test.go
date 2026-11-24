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
		{"2TB Batocera DOWNLOAD For Raspberry Pi 4 and P400", "", nil, []string{"Batocera"}, []string{"Raspberry Pi 4"}},
		{"1TB Batocera DOWNLOAD For Raspberry Pi 5", "", nil, []string{"Batocera"}, []string{"Raspberry Pi 5"}},
		{"1TB Batocera for Windows Desktop / Laptop - Plug and Play - Download", "", nil, []string{"Batocera"}, []string{"PC"}},
		// the label's tags
		{"Sample v42 – 256GB Batocera Build", "256GB-SampleV42-PC-STEAMDECK-XBOXALLYX-Batocera", nil,
			[]string{"Batocera"}, []string{"Steam Deck", "ROG Ally", "PC"}},
		{"128gb Pi4 & Pi3 Batocera 36 Image", "128GB-Pi4Pi336-RPI4-RPI3-Batocera", nil, []string{"Batocera"}, []string{"Raspberry Pi 4", "Raspberry Pi 3"}},
		// other names the same link was listed under count too
		{"1TB Retrobat DOWNLOAD for ROG ALLY, ROG ALLY X and AYAYNEO - Plug & Play", "", []string{"1TB Preloaded RetroBat Download for MSI Claw - Plug and Play"},
			[]string{"RetroBat"}, []string{"ROG Ally", "MSI Claw", "AYANEO"}},
		// Windows-only launchers are for a PC when nothing else is named ...
		{"8TB Hyperspin Download - Over 320+ Systems & 90,000+ Games", "", nil, []string{"HyperSpin"}, []string{"PC"}},
		{"20TB LaunchBox DOWNLOAD - PRE-CONFIGURED for Plug & Play!", "", nil, []string{"LaunchBox"}, []string{"PC"}},
		// ... but a named device wins
		{"16gb Pi4 Vertical Arcade Only Image", "16GB-Pi4VerticalArcadeOnlyImage-RPI4-HyperSpin", nil, []string{"HyperSpin"}, []string{"Raspberry Pi 4"}},
		{"1TB Teknoparrot with Retrobat - Lightgun Only Download", "", nil, []string{"RetroBat", "TeknoParrot"}, []string{"PC"}},
		// a size is not a model number; a Pi without a model is just a Raspberry Pi
		{"128gb Raspberry Pi image", "", nil, []string{}, []string{"Raspberry Pi"}},
		{"32gb FULLY LOADED RetroFlag GPI Case Pre-made image", "", nil, []string{}, []string{"Raspberry Pi"}},
		{"16gb MAME 2003 Raspberry Pi Image for the Rpi2-3b", "16GB-MAME2003RaspberryPi-RPI3-RetroPie", nil, []string{"RetroPie"}, []string{"Raspberry Pi 3", "Raspberry Pi 2"}},
		{"PiZero 32gb RETROPIE Image for the PI 0/PI 0 W/ Pi 1", "", nil, []string{"RetroPie"}, []string{"Raspberry Pi Zero", "Raspberry Pi 1"}},
		{"Pi Zero 2 W GPI Case RecalBox Image", "", nil, []string{"Recalbox"}, []string{"Raspberry Pi Zero"}},
		// words that are not what they look like
		{"[128GB] BATOCERA 5.20 RPI 3b+ DREAMCAST, N64, PSP VIDEOS GAMES 13000 + KODI No Recalbox", "", nil, []string{"Batocera"}, []string{"Raspberry Pi 3"}},
		{"16gb Raspberry Pi 4 LightGun Image WiiMote Ready", "", nil, []string{}, []string{"Raspberry Pi 4"}},
		{"PC Engine Collection", "", nil, []string{}, []string{}},
		{"STEAMY DECK build", "", nil, []string{}, []string{"Steam Deck"}},
		{"2TB XBOX360-RGH", "", nil, []string{}, []string{"Xbox 360"}},
		{"XBOX Master Game Collection [CCI]", "", nil, []string{}, []string{"Xbox"}},
		{"1.5TB Build DOWNLOAD for Retroid Pocket 4 Pro RP5, Odin, Anbernic RG556 Cube Mini and Android Handhelds", "", nil,
			[]string{"Android"}, []string{"Retroid Pocket", "Anbernic", "AYN Odin"}},
		{"Version 2 128gb Odroid Go Fully Loaded Batocera Image", "", nil, []string{"Batocera"}, []string{"Odroid Go"}},
		{"128gb Batocera Image for Odroid N2 or S905x", "", nil, []string{"Batocera"}, []string{"Odroid", "Amlogic TV box"}},
		{"Playstation Classic – Fully Loaded 128gb Build", "128GB-SampleBuild-PLAYSTATIONCLASSIC-EmulationStation", nil,
			[]string{"EmulationStation"}, []string{"PlayStation Classic"}},
		{"1TB XSTATION - DOWNLOAD - Complete PS1 Collection - NTSC", "", nil, []string{}, []string{"PlayStation (XStation)"}},
		{"MAME - DOWNLOAD - Complete - 0.258 Roms + CHDs", "", nil, []string{}, []string{}},
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
