package cinema

import (
	"strings"
	"testing"
)

// TestChannelCandidatesRootPath covers the "unknown" cascade's generated bare
// URL (rtsp://user:pass@ip:port/) — the case a manual mpv audit found was
// missing the single most common working path for these devices,
// "/cam/realmonitor?channel=N&subtype=0&unicast=true&proto=Onvif" (used by
// Dahua and many generic DVR/NVR/IPC clones sold under other brands), and
// never tried the bare root URL itself even though many single-channel
// devices serve video directly there.
func TestChannelCandidatesRootPath(t *testing.T) {
	fams := channelCandidates("rtsp://admin:admin123@1.2.3.4:554/")
	names := make([]string, len(fams))
	for i, f := range fams {
		names[i] = f.name
	}

	requireFamily := func(name string) candidateFamily {
		for _, f := range fams {
			if f.name == name {
				return f
			}
		}
		t.Fatalf("missing %q family; got families: %v", name, names)
		return candidateFamily{}
	}

	root := requireFamily("root")
	if len(root.urls) != 1 || root.urls[0] != "rtsp://admin:admin123@1.2.3.4:554/" {
		t.Fatalf("root family = %v, want a single bare-root URL", root.urls)
	}

	realmon := requireFamily("Dahua-realmonitor")
	if len(realmon.urls) != maxChannels {
		t.Fatalf("Dahua-realmonitor family has %d urls, want %d", len(realmon.urls), maxChannels)
	}
	want1 := "rtsp://admin:admin123@1.2.3.4:554/cam/realmonitor?channel=1&subtype=0&unicast=true&proto=Onvif"
	if realmon.urls[0] != want1 {
		t.Fatalf("Dahua-realmonitor first url = %q, want %q", realmon.urls[0], want1)
	}
	want2 := "rtsp://admin:admin123@1.2.3.4:554/cam/realmonitor?channel=2&subtype=0&unicast=true&proto=Onvif"
	if realmon.urls[1] != want2 {
		t.Fatalf("Dahua-realmonitor second url = %q, want %q", realmon.urls[1], want2)
	}
}

// TestChannelCandidatesRealmonitorRecognized covers a stored Link that's
// already a realmonitor URL (e.g. a moderator or the unknown cascade pinned
// it to channel 1) — should expand to the full channel range via the
// dedicated regex match, not fall through to a single literal "original" try.
func TestChannelCandidatesRealmonitorRecognized(t *testing.T) {
	fams := channelCandidates("rtsp://admin:admin@1.2.3.4:554/cam/realmonitor?channel=1&subtype=0&unicast=true&proto=Onvif")
	if len(fams) != 1 || fams[0].name != "Dahua-realmonitor" {
		t.Fatalf("expected a single Dahua-realmonitor family, got %+v", fams)
	}
	if len(fams[0].urls) != maxChannels {
		t.Fatalf("got %d urls, want %d", len(fams[0].urls), maxChannels)
	}
}

// TestChannelLabelRealmonitorQuery covers labelling realmonitor candidates:
// the channel number lives in the query string, not the path, so every
// candidate shares the identical path ("/cam/realmonitor") and would
// otherwise get the same generic label.
func TestChannelLabelRealmonitorQuery(t *testing.T) {
	got := channelLabel("/cam/realmonitor?channel=3&subtype=0&unicast=true&proto=Onvif")
	if got != "CH3" {
		t.Fatalf("channelLabel = %q, want %q", got, "CH3")
	}
}

// TestChannelLabelUnaffectedByEmptyQuery guards the anchored path regexes
// (StdCh/numeric use ^ and $) against the new "path?query" calling
// convention: a bare path with no query must label identically to before.
func TestChannelLabelUnaffectedByEmptyQuery(t *testing.T) {
	cases := map[string]string{
		"/StdCh3?":                  "CH3",
		"/7?":                       "CH7",
		"/Streaming/Channels/101?":  "CH1 Main",
		"/h264/ch2/main/av_stream?": "CH2 Main",
	}
	for in, want := range cases {
		if got := channelLabel(in); got != want {
			t.Errorf("channelLabel(%q) = %q, want %q", in, got, want)
		}
	}
	// And confirm the bare (no "?") form still works identically.
	if got := channelLabel("/StdCh3"); got != "CH3" {
		t.Fatalf("channelLabel without query = %q, want CH3", got)
	}
}

func TestReDahuaRealmonitorMatches(t *testing.T) {
	if !reDahuaRealmonitor.MatchString("/cam/realmonitor") {
		t.Fatal("expected /cam/realmonitor to match")
	}
	if !strings.Contains(strings.ToLower("/CAM/RealMonitor"), "realmonitor") {
		t.Fatal("sanity check on test data itself failed")
	}
}
