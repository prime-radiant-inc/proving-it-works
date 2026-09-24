package term

import "testing"

func TestParseTitle(t *testing.T) {
	for _, c := range []struct {
		title     string
		seq, code int
		ok        bool
	}{
		{"MOVIE;3;0", 3, 0, true},
		{"MOVIE;12;127", 12, 127, true},
		{"bash", 0, 0, false},
		{"MOVIE;x;0", 0, 0, false},
	} {
		seq, code, ok := parseTitle(c.title)
		if seq != c.seq || code != c.code || ok != c.ok {
			t.Errorf("parseTitle(%q) = %d %d %v", c.title, seq, code, ok)
		}
	}
}

func TestParseStatusSplitsStatusLineFromScreen(t *testing.T) {
	st, err := parseStatus("2;1;120;34;1;on;0;4;bash;MOVIE;5;1\n$ false\n$\n")
	if err != nil {
		t.Fatal(err)
	}
	want := Status{CursorX: 2, CursorY: 1, Cols: 120, Rows: 34, CursorVisible: true, Film: true,
		Sent: 4, Seq: 5, Code: 1, Command: "bash", Screen: "$ false\n$\n"}
	if st != want {
		t.Fatalf("got %+v", st)
	}
	if !st.AtPrompt() {
		t.Fatal("a new prompt since the last input should be AtPrompt")
	}
}

func TestParseStatusReadsStop(t *testing.T) {
	st, err := parseStatus("0;0;80;24;0;on;1;0;bash;bash\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Stop {
		t.Fatalf("got %+v, want Stop true", st)
	}
	st, err = parseStatus("0;0;80;24;0;on;0;0;bash;bash\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if st.Stop {
		t.Fatalf("got %+v, want Stop false", st)
	}
}

func TestNotAtPromptUntilANewPromptArrives(t *testing.T) {
	if (Status{Seq: 4, Sent: 4, Command: "bash"}).AtPrompt() {
		t.Fatal("no new prompt since input was sent")
	}
	if (Status{Seq: 5, Sent: 4, Command: "vim"}).AtPrompt() {
		t.Fatal("a program is in the foreground")
	}
}
