package cinema

import "testing"

// TestDahuaAudioCodecSupported covers the shared support-check used by
// dahuaChannelAudio (sidebar "audio available" indicator) — must match every
// codec the live pipeline (dhavAudioPayload) actually plays: alaw/mulaw/s16le
// (as pcm16) and aac. "G.711A"/"G.711Mu"/"AAC" are confirmed live against a
// real fleet; "G.711U" is an unconfirmed defensive guess (never observed;
// "G.711Mu" is the real mu-law string). s16le/raw-PCM's Encode-config name
// is unconfirmed, so nothing maps to it yet — PCM stays unsupported here on
// purpose, not an oversight.
func TestDahuaAudioCodecSupported(t *testing.T) {
	cases := []struct {
		compression string
		want        bool
	}{
		{"G.711A", true},
		{"G.711Mu", true},
		{"G.711U", true},
		{"AAC", true},
		{"PCM", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := dahuaAudioCodecSupported(tc.compression); got != tc.want {
			t.Errorf("dahuaAudioCodecSupported(%q) = %v, want %v", tc.compression, got, tc.want)
		}
	}
}

// dahuaEncodeConfigFixture builds a configManager.getConfig("Encode") params
// payload for one channel, shaped exactly as confirmed live against a real
// Dahua fleet: MainFormat/ExtraFormat arrays of encode profiles (General,
// Motion Detect, Alarm — only index 0/"General" matters for live viewing),
// each with a sibling AudioEnable bool and an Audio.Compression string.
func dahuaEncodeConfigFixture(mainAudioEnable bool, mainCompression string, extraAudioEnable bool, extraCompression string) string {
	fmtEntry := func(enable bool, compression string) string {
		return `{"Audio":{"AudioSource":"BNC","BitRate":0,"Compression":"` + compression + `","Depth":16,"Frequency":8000,"Mode":0,"Pack":"DHAV","PacketPeriod":0},"AudioEnable":` + boolStr(enable) +
			`,"Video":{"BitRate":1024,"Compression":"H.265","FPS":15,"Height":1440,"Width":1280},"VideoEnable":true}`
	}
	// Three profiles per format array (General/Motion Detect/Alarm), matching
	// real captures — only the first ("General") is inspected by the parser,
	// the other two are included with the opposite AudioEnable value to prove
	// the parser really reads index 0, not just "any enabled entry".
	main := fmtEntry(mainAudioEnable, mainCompression) + "," + fmtEntry(!mainAudioEnable, mainCompression) + "," + fmtEntry(!mainAudioEnable, mainCompression)
	extra := fmtEntry(extraAudioEnable, extraCompression) + "," + fmtEntry(!extraAudioEnable, extraCompression) + "," + fmtEntry(!extraAudioEnable, extraCompression)
	return `{"table":{"MainFormat":[` + main + `],"ExtraFormat":[` + extra + `],"SnapFormat":[]}}`
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestParseDahuaEncodeAudio covers the real field shape confirmed live
// (MainFormat[0]/ExtraFormat[0], sibling AudioEnable, Audio.Compression),
// codec support gating, and graceful handling of malformed/absent data.
func TestParseDahuaEncodeAudio(t *testing.T) {
	t.Run("main and sub both enabled, supported codec", func(t *testing.T) {
		main, sub := parseDahuaEncodeAudio([]byte(dahuaEncodeConfigFixture(true, "G.711A", true, "G.711A")))
		if !main || !sub {
			t.Fatalf("main=%v sub=%v, want true,true", main, sub)
		}
	})

	t.Run("main enabled, sub disabled", func(t *testing.T) {
		main, sub := parseDahuaEncodeAudio([]byte(dahuaEncodeConfigFixture(true, "G.711A", false, "G.711A")))
		if !main || sub {
			t.Fatalf("main=%v sub=%v, want true,false", main, sub)
		}
	})

	t.Run("enabled but unsupported codec stays false", func(t *testing.T) {
		main, sub := parseDahuaEncodeAudio([]byte(dahuaEncodeConfigFixture(true, "PCM", true, "PCM")))
		if main || sub {
			t.Fatalf("main=%v sub=%v, want false,false (unsupported codec)", main, sub)
		}
	})

	t.Run("AAC counts as available, not just G.711", func(t *testing.T) {
		main, sub := parseDahuaEncodeAudio([]byte(dahuaEncodeConfigFixture(true, "AAC", true, "AAC")))
		if !main || !sub {
			t.Fatalf("main=%v sub=%v, want true,true (AAC is playable via the live pipeline)", main, sub)
		}
	})

	t.Run("G.711Mu (real mu-law name) counts as available", func(t *testing.T) {
		main, sub := parseDahuaEncodeAudio([]byte(dahuaEncodeConfigFixture(true, "G.711Mu", true, "G.711Mu")))
		if !main || !sub {
			t.Fatalf("main=%v sub=%v, want true,true", main, sub)
		}
	})

	t.Run("both disabled", func(t *testing.T) {
		main, sub := parseDahuaEncodeAudio([]byte(dahuaEncodeConfigFixture(false, "G.711A", false, "G.711A")))
		if main || sub {
			t.Fatalf("main=%v sub=%v, want false,false", main, sub)
		}
	})

	t.Run("malformed JSON does not panic and yields false,false", func(t *testing.T) {
		main, sub := parseDahuaEncodeAudio([]byte(`not json`))
		if main || sub {
			t.Fatalf("main=%v sub=%v, want false,false", main, sub)
		}
	})

	t.Run("empty table (device doesn't support this config) yields false,false", func(t *testing.T) {
		main, sub := parseDahuaEncodeAudio([]byte(`{"table":{}}`))
		if main || sub {
			t.Fatalf("main=%v sub=%v, want false,false", main, sub)
		}
	})
}
