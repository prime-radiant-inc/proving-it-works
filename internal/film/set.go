package film

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/prime-radiant-inc/proving-it-works/internal/exitcode"
	"github.com/prime-radiant-inc/proving-it-works/internal/jsonl"
)

// Failed is an action the app would not let happen: a target that is not
// there or is covered, a wait that timed out, a URL that would not load.
// It is the app's verdict, not a mistake in the command (exit 1).
type Failed struct{ Msg string }

func (e Failed) Error() string { return e.Msg }

// Set keeps the bookkeeping of a session whose recorder films pictures
// (browse, desk) in Dir: which take is being filmed and whether filming is
// on (state.json), when filming and actions happen (marks.jsonl, which only
// the verbs write), and what was said (beats.jsonl). The recorder writes
// frames.jsonl; Render merges the two.
type Set struct{ Dir string }

// state is what the verbs remember between them: the number of the take
// being filmed (from 1), whether filming is on, and whether the last action
// ended a narrated beat, so the next one cuts.
type state struct {
	Take       int  `json:"take"`
	Film       bool `json:"film"`
	CutPending bool `json:"cut_pending"`
}

func (s Set) state() (state, error) {
	var st state
	data, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(data, &st)
}

func (s Set) setState(st state) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.Dir, "state.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Dir, "state.json"))
}

// Now is the clock marks and frames are stamped with: seconds since 1970.
func Now() float64 { return float64(time.Now().UnixNano()) / 1e9 }

func (s Set) mark(t float64, film, busy bool) error {
	return jsonl.Append(filepath.Join(s.Dir, "marks.jsonl"), Mark{T: t, Film: film, Busy: busy})
}

// Open starts the bookkeeping: take 1, filming off, and the app busy
// starting up, so nothing before Roll is filmed.
func (s Set) Open() error {
	if err := s.setState(state{Take: 1}); err != nil {
		return err
	}
	return s.mark(Now(), false, true)
}

// Roll starts filming take 1, with the app ready and waiting.
func (s Set) Roll() error {
	if err := s.setState(state{Take: 1, Film: true}); err != nil {
		return err
	}
	return s.mark(Now(), true, false)
}

// End marks the end of the session.
func (s Set) End() error {
	return jsonl.Append(filepath.Join(s.Dir, "marks.jsonl"), Mark{T: Now(), End: true})
}

// SetFilm turns filming on or off; each on after an off starts a new take.
func (s Set) SetFilm(on bool) error {
	st, err := s.state()
	if err != nil {
		return err
	}
	return s.setFilm(&st, on)
}

func (s Set) setFilm(st *state, on bool) error {
	if st.Film == on {
		return nil
	}
	if on {
		// a new take starts here, which is all a pending cut would do
		st.Take++
		st.CutPending = false
	}
	st.Film = on
	if err := s.mark(Now(), on, false); err != nil {
		return err
	}
	return s.setState(*st)
}

// Cut ends the current take, holding its last picture, and starts the next.
func (s Set) Cut() error {
	st, err := s.state()
	if err != nil {
		return err
	}
	return s.cut(&st)
}

// cut ends the take being filmed and starts the next. With filming off
// there is no take to end, and the next film on starts a new one anyway.
func (s Set) cut(st *state) error {
	st.CutPending = false
	if !st.Film {
		return s.setState(*st)
	}
	if err := s.setFilm(st, false); err != nil {
		return err
	}
	return s.setFilm(st, true)
}

// Act runs do as one action. If the previous action ended a narrated beat,
// it first cuts to a new take. The app is marked busy from the start of the
// action until settle returns; a failed action's time counts as waiting, so
// a timed-out wait leaves no dead air. With say, a successful action ends a
// beat: say narrates everything filmed since the previous beat ended, and
// the next action starts a new take. It returns do's line saying what it
// acted on, and the exit code: 1 when do fails with Failed.
func (s Set) Act(say string, do func() (string, error), settle func()) (string, int, error) {
	st, err := s.state()
	if err != nil {
		return "", exitcode.Usage, err
	}
	if say != "" && !st.Film {
		return "", exitcode.Usage, errFilmOff
	}
	if st.CutPending {
		if err := s.cut(&st); err != nil {
			return "", exitcode.Usage, err
		}
	}
	began := Now()
	if err := s.mark(began, st.Film, true); err != nil {
		return "", exitcode.Usage, err
	}
	did, actErr := do()
	var failed Failed
	if errors.As(actErr, &failed) {
		// the busy mark and this one share a time; this one, written later, wins
		if err := s.mark(began, st.Film, false); err != nil {
			return "", exitcode.Usage, err
		}
		return "", exitcode.Verdict, actErr
	}
	if actErr == nil {
		settle()
	}
	if err := s.mark(Now(), st.Film, false); err != nil {
		return "", exitcode.Usage, err
	}
	if actErr != nil {
		return "", exitcode.Usage, actErr
	}
	if say != "" {
		if err := s.Say(say); err != nil {
			return "", exitcode.Usage, err
		}
	}
	return did, exitcode.OK, nil
}

// Say ends a beat now: the sentence narrates everything filmed since the
// previous beat ended, and the next action starts a new take.
func (s Set) Say(sentence string) error {
	st, err := s.state()
	if err != nil {
		return err
	}
	if !st.Film {
		return errFilmOff
	}
	if err := AppendBeat(s.Dir, Beat{Take: st.Take, Say: sentence}); err != nil {
		return err
	}
	st.CutPending = true
	return s.setState(st)
}

var errFilmOff = errors.New("filming is off, so nothing would show what the sentence describes: " +
	"film on, then narrate an action that shows the result (wait for it, if need be)")
