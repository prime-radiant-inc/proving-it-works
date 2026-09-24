package srt

import "testing"

func end(t *testing.T, text string) float64 {
	t.Helper()
	got, err := End(text)
	if err != nil || got == nil {
		t.Fatalf("End(%q) = %v, %v", text, got, err)
	}
	return *got
}

func TestLiteralArrowInCaptionIsNotATimingLine(t *testing.T) {
	if got := end(t, "1\n00:00:00,000 --> 00:00:01,250\nFollow source --> destination.\n"); got != 1.25 {
		t.Fatal(got)
	}
}

func TestTimestampShapedCaptionCannotExtendCoverage(t *testing.T) {
	if got := end(t, "1\n00:00:00,000 --> 00:00:01,250\n00:00:00,000 --> 00:59:00,000\n"); got != 1.25 {
		t.Fatal(got)
	}
}

func TestMalformedTimingIsRejected(t *testing.T) {
	for _, timing := range []string{"00:00:00,000 --> invalid", "not a timing line", "00:00:00,000 --> 00:99:00,000"} {
		if _, err := End("1\n" + timing + "\ncaption\n"); err == nil {
			t.Errorf("accepted %q", timing)
		}
	}
}

func TestEmptySubtitlesHaveNoEnd(t *testing.T) {
	got, err := End("\n  \n")
	if err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
}

func TestLatestEndWinsAcrossCues(t *testing.T) {
	text := "1\n00:00:00,000 --> 00:00:10,250\nFirst\n\n2\n00:00:05,000 --> 00:00:06,000\nSecond\n"
	if got := end(t, text); got != 10.25 {
		t.Fatal(got)
	}
}

func TestByteOrderMarkAndCRLFAreAccepted(t *testing.T) {
	if got := end(t, "\ufeff1\r\n00:00:00,000 --> 00:00:02,000\r\nλ caption\r\n"); got != 2 {
		t.Fatal(got)
	}
}
