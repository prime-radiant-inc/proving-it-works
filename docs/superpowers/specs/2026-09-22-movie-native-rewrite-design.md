# `movie`: a native rewrite of the proving-it-works tools

Date: 2026-09-23
Status: design, awaiting review

## Goal

A simple set of tools any agent can use to make a movie proving that
something works. Every feature earns its place against that goal.

**What is code and what is instructions.** Code is for work an agent gets
wrong silently and repeatedly, or arithmetic it cannot do by eye: judging a
finished movie's timeline, timing a cut to measured narration, and filming a
terminal. Everything an agent can do correctly after reading a clear
paragraph stays in the skill as instructions: driving a browser, capturing a
desktop window, rendering a reel from a log, and composing stills.

## Why rewrite

The pipeline today is five Python scripts the agent must run in order with
matching flags, handing state through three files each tool reads its own
way. There is no shared core, the scene file is never validated, the
terminal route is a prose recipe on Unix and a 632-line daemon on Windows,
and a third of the docs are PowerShell and Git Bash copies of the pipeline
because `uv` shebangs do not run on Windows.

## Decisions

| Decision | Choice |
|---|---|
| Language | Go, one binary. |
| Distribution | Prebuilt binaries committed to the repo. The skill never downloads executables at run time. |
| Speech recognition | None. |
| Engines | `openai`, `openai-chat`, `piper`. |
| Scene file | New format, no backward compatibility. |
| Terminal | tmux holds the session; `movie term` drives it, records styled snapshots, and renders frames. macOS, Linux, and WSL; no native Windows terminal filming. |
| Browser apps | `movie browse` drives headless Chrome through a small CDP client, draws the cursor, and films the screencast, the same shape as `term`. Playwright stays in the skill as the escape hatch. |
| Desktop capture, log reels, stills | Instructions in the skill, not code. |

## Commands

| Command | Does | Platforms |
|---|---|---|
| `movie build SCENES.yaml OUT.mp4` | narrate, assemble, subtitle, burn, check | all |
| `movie check MOVIE` | the gate, on any movie | all |
| `movie term start / run / type / key / wait / screen / film / stop / render` | drive a terminal session and render it into frames | macOS, Linux, WSL |
| `movie browse start / goto / click / type / choose / press / wait / page / cut / film / stop / render` | drive a web app in headless Chrome and render it into frames | macOS, Linux, WSL |

Exit codes: 0 success; 1 negative verdict (not shippable, narration
rejected, a filmed command failed); 2 usage or environment error (including
`run` refused because a command is still running); 3 `term` only, the
command is still running when `run` or `wait` returns.

## Layout and shipping

```
cmd/movie/                  flag parsing and dispatch
internal/scene/             parse and validate the scene file
internal/ffmpeg/            run ffmpeg/ffprobe
internal/fonts/             embedded fonts, fallback chain, coverage checks
internal/narrate/           engines, clip cache, openai-chat gate
internal/build/             segments, cards, concat, subtitles, burn
internal/check/             sampling and the verdict
internal/term/              tmux control, recorder, snapshot renderer
skills/proving-it-works-with-a-movie/bin/
    movie                   sh launcher (macOS, Linux, Git Bash)
    movie-darwin-arm64  movie-darwin-amd64  movie-linux-amd64
    movie-linux-arm64   movie-windows-amd64.exe
script/build-binaries
```

Decisions are pure functions over plain data (the check verdict, cue timing,
the openai-chat gate, scene validation, prompt-marker parsing, SGR parsing,
frame timing), tested directly with no mocks.

**Launcher.** `bin/movie` maps `uname` to a binary and `exec`s it with
`"$@"`. On Git Bash (`MINGW*`/`MSYS*`/`CYGWIN*`) it first exports
`MSYS_NO_PATHCONV=1` and `MSYS2_ARG_CONV_EXCL='*'` so Git Bash does not
rewrite Unix-looking arguments; paths from Git Bash are passed in Windows
form (`cygpath -m`). PowerShell runs the `.exe` directly. No `.cmd`
launcher, because cmd.exe mangles arguments.

**Build.** `script/build-binaries` builds all five with `GOTOOLCHAIN` set to
the exact version in `go.mod` (the `toolchain` line if present, else the
`go` line, which is kept at a full patch version such as `go 1.26.1`),
`GOAMD64=v1`, `GOARM64=v8.0`, `CGO_ENABLED=0`, `-trimpath`,
`-buildvcs=false`, `-ldflags "-s -w"`. `.gitattributes` forces LF on sources
and marks `bin/*` and fonts binary. A test rebuilds all five and
byte-compares them with the committed ones; CI runs it on `main`. Binaries
are rebuilt and committed when work lands on `main`, not on every commit of
a work branch, so the repository grows by one set of binaries per release
rather than one per commit.

**Prerequisites.** ffmpeg and ffprobe everywhere. tmux for `term` (inside
the container too, when filming one). For the keyless voice, Piper
(`uv tool install piper-tts`) and a voice (`uvx --from piper-tts python -m
piper.download_voices --data-dir DIR VOICE`). The tool never installs
anything, and a missing prerequisite is an error naming the command.
Chrome, Chromium, or Edge for `browse`.

**Fonts.** Embedded, under their permissive licenses: DejaVu Sans for cards
and burned subtitles, DejaVu Sans Mono for terminals, plus fallback fonts
for the symbols CLIs print (Claude Code's `⏺` and `⎿`, `⏵`, `⏸`, braille
spinners, box drawing), chosen in the `term` spike. No CJK or emoji.

## The scene file

```yaml
size: 1920x1080              # default
fps: 30                      # default
engine: auto                 # auto | openai | openai-chat | piper
voice: nova                  # default: nova (openai*), en_US-lessac-medium (piper)

scenes:
  - id: title
    card: proving-it-works
    subtitle: installed from a public marketplace
    duration: 4

  - id: install
    frames: takes/install/take-1/
    rate: 10
    narration: This is a container with nothing of ours in it.

  - id: sheet
    image: work/out/contact-sheet.png
    narration: So we do.

  - id: movie
    movie: work/out/counter.mp4
```

| Field | Kinds | Default |
|---|---|---|
| `id` | all | required; `[a-z0-9][a-z0-9-]*`, unique |
| `card` (title), `subtitle` | card | |
| `image` | image | |
| `frames`, `rate` | frames | `rate` defaults to `fps` |
| `movie` | movie | |
| `duration` | card, image | 3; the scene lasts max(narration, `duration`) |
| `narration` | card, image, frames | none |

A scene's kind is whichever of `card`, `image`, `frames`, or `movie` it
has. Paths are relative to the scene file. `engine: auto` picks `openai`
when `OPENAI_API_KEY` (or `llm keys get openai`) yields a key, else `piper`.

Validation runs before any work and reports every problem at once with its
scene id: unknown keys, zero or two kinds, a field on a kind that does not
take it, bad or duplicate ids, missing files, empty frame directories,
non-positive numbers, and card text containing a character the card font
cannot draw (named, with its code point).

## `build`

`movie build demo.yaml demo.mp4` writes `demo.mp4`, `demo.srt` (when
narrated), and `demo-check/contact-sheet.png`, and keeps scratch in
`demo.build/` beside the output.

1. **Validate.**
2. **Narrate** each narrated scene. The clip is
   `demo.build/narration/<hash of text, engine, voice, model>.wav`. If it
   exists it is reused. Otherwise synthesize to a temp file, apply the
   `openai-chat` gate if that engine is used, and rename into place only if
   accepted; two attempts, then exit 1 with the reason and, for
   `openai-chat`, the transcript. A clip exists only if it was accepted, so
   there is no manifest to keep consistent. To redo a clip that sounds
   wrong, delete it; `build` prints each clip's path.
3. **Assemble** each scene into `demo.build/<id>.mp4` at `size` and `fps`.
   Frames and images are piped to ffmpeg's stdin (`image2pipe`), so no user
   path is ever parsed as an ffmpeg pattern; an image is a one-frame
   sequence. The last frame is held to max(narration, visuals or
   `duration`), and short audio pads with silence. Cards are drawn in Go
   (title and subtitle, centered, wrapped at spaces, shrunk until the
   longest line fits). `movie` scenes are scaled to fit and keep their own
   audio, with a silent track added if they have none. Segments are
   concatenated from a list of their fixed safe names, run from the scratch
   directory, so nothing needs escaping.
4. **Subtitle**, only if anything is narrated: each narrated scene's cues
   span its narration clip, starting at the scene's offset in the cut
   (today's millisecond allocation), written to `demo.srt`.
5. **Burn**, only if anything is narrated: copy the SRT and the embedded
   DejaVu Sans into scratch and burn with
   `subtitles=filename=captions.srt:fontsdir=.`, boxed as today, so the
   result does not depend on system fonts. If ffmpeg lacks the `subtitles`
   filter, embed a soft track instead and print a `WARN` saying the
   subtitles are not in the picture. A burn that fails with the filter
   present is an error.
6. **Check** the result, expecting audio and subtitles exactly when
   something is narrated, and comparing subtitle coverage with the end of
   the last narration clip in the cut.

### The `openai-chat` gate

Kept exactly from today: normalize script and returned transcript (NFKC,
casefold, letters, numbers, and marks only); reject when the transcript is
empty or when `|len(want) - len(got)| + positional mismatches` exceeds
`max(2, len(want) // 25)`; reject scripts that cannot be split into words.
It proves what the model says it said, not what is in the WAV, and the docs
say so.

`piper` runs `piper -m VOICE --data-dir DIR -i TEXT.txt -f OUT.wav`, text in
a UTF-8 file. Voices live in `PIPER_VOICE_DIR`, default
`~/.cache/piper-voices`.

## `check`

Today's `check-movie`, ported as is: 1 Hz colour samples, 320 px wide; a
second counts as a change when more than 0.2% of pixels move by more than 8
grey levels; 1-second RMS windows, speech at -45 dBFS or louder; today's
failures (not a movie, never changes, front-loaded action, no audio, silent,
narrated without subtitles, subtitles ending early) and warnings; the
contact sheet with today's grid rule; `check.json` always written.

`movie check MOVIE [--no-expect-audio] [--no-expect-subtitles]`. Subtitles
come from `<movie stem>.srt` beside the movie, else an embedded track, and
are required only when speech is heard, as today.

## `term`

tmux holds the shell, answers terminal queries, encodes keys, and keeps the
session alive between the agent's tool calls. `movie term` drives it, records
what it shows, and renders frames. The agent drives the session with its own
tool calls, a sub-agent, or a bespoke script, reacting to what `screen`
shows; this is for one-off movies.

### Verbs

| Verb | Does |
|---|---|
| `start SESSION [--cwd DIR] [--size 120x34] [-- WRAPPER...]` | create the session and start the recorder in the background, then return |
| `run SESSION 'cmd' [--timeout 60]` | refuse (exit 2) unless the shell is at a prompt; type the command at human pace; Enter; wait for the next prompt; print the exit code and the screen text |
| `type SESSION 'text'` | type into whatever is running, at human pace, with no prompt check |
| `key SESSION NAME` | `Enter`, `Escape`, `Tab`, `Up`, `Down`, `Left`, `Right`, `C-c`, or one character, via `tmux send-keys` |
| `wait SESSION [--quiet S] [--timeout 60]` | wait for the next prompt (exit 0 or 1 with the shell's status), or for the screen to stay unchanged for `--quiet` seconds (exit 3, still running); print the screen text |
| `screen SESSION` | print the current screen as text |
| `film SESSION on\|off` | filming is on at start; `off` keeps what follows out of the movie, and each `on` starts a new take |
| `stop SESSION OUTDIR` | end the session, stop the recorder, render the takes |
| `render SESSION OUTDIR [--px 1600x900]` | render the takes again from the recording, even after the session is gone |

`start` and `stop` refuse a non-empty `SESSION` or `OUTDIR`.

**The session.** `start` runs a private tmux server on a socket in `/tmp`
named by a hash of the session directory (Unix socket paths are limited to
about 100 bytes), started with `-f /dev/null` so it never reads the user's
tmux configuration or touches their tmux. In it runs
`bash --noprofile --norc -i` under `env`, which removes `TERM_PROGRAM`,
`TERM_PROGRAM_VERSION`, `TERM_SESSION_ID`, `TMUX`, and the launcher's
`MSYS_*` variables, keeps the target's own `HOME` and `PATH`, and sets
`TERM=xterm-256color`, `BASH_SILENCE_DEPRECATION_WARNING=1` (no macOS zsh
banner), `HISTFILE` to a session file (never the user's history), `PS1`,
and a `PROMPT_COMMAND` that captures `$?`, runs
`export -n PROMPT_COMMAND` so nested shells do not inherit it, and sets the
title to `MOVIE;<sequence>;<status>` (verified with bash 3.2 and tmux 3.5a:
the marker appears in `#{pane_title}`). After the first prompt, `start`
clears the screen, then starts recording, so the first frame is a clean
prompt.

**The recorder** is `movie term start` re-executed as a detached background
process, so `start` returns at once and no harness needs to keep a task
alive. It polls `tmux capture-pane -p -e` (text with colour codes), the
cursor position, and the pane title as fast as tmux answers, up to 10 times
a second, and appends each changed snapshot with its timestamp and the
current `film` state to `SESSION/recording.jsonl`. It exits when the tmux
server does.

**Readiness and status.** A prompt has returned when the title's sequence
number has advanced and `#{pane_current_command}` is `bash`. `run` refuses
unless that holds; typing only ever happens at a prompt, so keys cannot land
in a running program's stdin.

**Containers.** With `-- WRAPPER...`, such as `-- docker exec CONTAINER`,
every tmux command runs through the wrapper, so tmux and the shell live in
the container while `movie` and the recording stay on the host. `--cwd` is
then a path inside the container. The container needs tmux, as today's demo
image has.

**Rendering.** `render` (and `stop`) turn `recording.jsonl` into
`OUTDIR/take-1/`, `take-2/`, ...: one PNG directory per filmed stretch at 10
fps, each snapshot held until the next, each with a `take.json` holding a
ready-to-paste `frames` scene. A snapshot is drawn cell by cell from its
text and colour codes (tmux emits only SGR sequences in `capture-pane -e`),
with wide characters taking two cells and a block cursor. Glyphs come from
DejaVu Sans Mono, then the fallback fonts; a character none of them has is
drawn as a visible replacement box, and `render` prints a `WARN` listing
every such character with its code point, so a missing glyph is never
silent. Time is never compressed: long waits belong behind `film off`.

**Cleanup.** `stop` kills the private tmux server, which ends the shell and
everything it started; the recorder then exits. Processes a wrapper started
inside a container end with that container's tmux server.

### Spike first

Before the plan's `term` work, a short spike confirms, on macOS and Linux:
the prompt marker and readiness rule under `capture-pane` polling; capture
rates achievable locally and through `docker exec`; SGR coverage in
`capture-pane -e` output from real sessions (a plain shell, `htop` or
`less`, and a Claude Code session); and which fallback fonts cover what those
sessions print.

## `browse`

`movie browse` films a web app the way `term` films a shell: the agent
drives a real Chrome one verb at a time, a detached recorder films the page,
and `stop` renders the takes and writes the scene file. It replaces
hand-written Playwright for the common case; the skill keeps Playwright as
the escape hatch for drag and drop, uploads, iframes, and complex logins.

### Verbs

| Verb | Does | Exit |
|---|---|---|
| `start SESSION URL [--size 1280x720] [--title T] [--subtitle S] [--browser PATH]` | launch Chrome, start the recorder, load URL, then return | 0, or 2 |
| `goto SESSION URL [--say S]` | load URL and wait for it | 0, 1 load failed |
| `click SESSION TARGET [--say S]` | glide the cursor to TARGET and click it | 0, 1 not found or covered |
| `type SESSION TARGET 'text' [--replace] [--say S]` | click TARGET, then type at human pace after its text, or over it | 0, 1 |
| `choose SESSION TARGET 'Option' [--say S]` | pick an option in a select (headless Chrome draws no popup, so this sets it and fires input and change) | 0, 1 |
| `press SESSION KEY [--say S]` | `Enter`, `Tab`, `Escape`, `Backspace`, `Delete`, `Home`, `End`, `Up`, `Down`, `Left`, `Right`, or one character | 0 |
| `wait SESSION TARGET [--timeout 10] [--say S]` | wait until TARGET is visible, then scroll it into view | 0, 1 timed out |
| `page SESSION` | print the URL, title, visible text, and the targets on the page | 0 |
| `cut SESSION`, `film SESSION on\|off` | as in `term` | 0 |
| `stop SESSION OUTDIR` | close Chrome, render the takes, write `OUTDIR/scenes.yaml` | 0 |
| `render SESSION OUTDIR` | render again from the recording | 0 |

A TARGET is a CSS selector, or `text=Label`: the visible element whose
text, `aria-label`, label, placeholder, title, or button value is Label,
ignoring case and extra spaces. For acting (click, type, choose), buttons,
links, and fields are tried first, exact then whole-word, then any element,
exact then whole-word; for `wait`, any element, exact then whole-word.
Whole-word means "Saved" matches "Saved 2" but not "Unsaved". Within a
tier, an element a click would reach beats one covered by another. A target
that matches nothing fails with the page's targets listed, so the next
attempt is informed. A click whose point is covered fails, naming the cover.

After every action the verb waits for the page to settle (loaded, and no DOM
change for 400 ms counted from the end of the action, at most 5 s), then
prints what it acted on and the URL and title. A URL differing only in its
fragment is a same-document navigation with nothing to load. `page` is how
the agent reads the page, field values included.

`--say` works as in `term`: it ends a beat, its sentence narrates everything
filmed since the previous `--say`, and the next action starts a new take.
It is refused while filming is off, and a pending cut does nothing while
filming is off. A failed action's time counts as waiting, so it is cut.

### How it works

**Chrome.** `start` finds Chrome, Chromium, or Edge (`--browser` wins; then
the standard macOS application paths; then `google-chrome`,
`google-chrome-stable`, `chromium`, `chromium-browser`, `microsoft-edge` on
PATH) and launches it detached: `--headless=new`, a fresh profile in
`SESSION/profile`, `--remote-debugging-port=0` (Chrome writes the port it
chose to `DevToolsActivePort` in the profile), `--window-size` from
`--size`, `--force-device-scale-factor=1.25` so a 1280x720 viewport films at
1600x900, `--hide-scrollbars`, `--mute-audio`, and `--no-sandbox` only when
running as root. It runs on macOS, Linux, and WSL.

**CDP.** `internal/cdp` is a small DevTools Protocol client over
`github.com/coder/websocket`: one browser-level connection, flat sessions
(`Target.attachToTarget` with `flatten`), calls matched to replies by id,
and events delivered to a callback. chromedp was rejected: it adds about
6 MB to each committed binary, and it closes tabs it attaches to.

**Processes.** The recorder (`movie browse _record SESSION`) attaches to the
page for the whole session: it adds the overlay script to every new
document, starts `Page.startScreencast` (PNG, frames only when the page
changes), writes each frame to `SESSION/frames/NNNNNN.png`, and appends
`{t, frame}` to `frames.jsonl`, `t` being Chrome's frame timestamp. Each
verb is a short process that attaches to the same page, acts, and exits
without closing it (verified by spike). Verbs append `{t, film, busy, end}`
marks to `marks.jsonl`: busy from the start of an action until it settles,
film as set by `film` and `cut`. Take number and a pending cut live in
`SESSION/state.json`; beats in `beats.jsonl` as in `term`. `stop` marks the
end, writes `SESSION/stop`, waits for the recorder's `recorder.done`, closes
Chrome with `Browser.close` (killing it if it lingers), and removes the
profile; a second `stop` is refused. A failed `start` empties the session
directory again.

**The overlay** is a script added to every top-level document (never
frames: one cursor). It draws an arrow
cursor (fixed position, top z-index, no pointer events) that glides to each
click target with easing and pulses on the click, remembers its position in
`sessionStorage` so it does not jump on same-origin navigation, counts DOM
mutations for the settle check, and implements TARGET lookup, so the rules
live in one place.

**Clicks and keys** are real input events (`Input.dispatchMouseEvent`,
`Input.dispatchKeyEvent`), so every framework sees what a user's input
would produce.

**Rendering.** Frames and marks merge by time into one timeline. Takes,
narration, waiting-time compression, and the scene file are shared with
`term` in `internal/film`: time the agent spends between verbs (not busy)
while the page holds still is cut to 1.5 s, time the app spends working is
never cut, and each take holds its final picture at least 1.5 s.

## `desk`

`movie desk` films a desktop app on an X11 display the way `browse` films a
web page: the agent looks (`shot`), acts with real pointer and key input,
and narrates with `--say`; a detached recorder films the screen. Linux and
X11 only for now: moving the real pointer on macOS needs native code the
static release build cannot carry.

| Verb | Does | Exit |
|---|---|---|
| `start SESSION [--display :99] [--window NAME] [--title T] [--subtitle S] [-- WRAPPER...]` | film the display, or one window's area; returns once filming | 0, or 2 |
| `shot SESSION [PNG]` | save what is filmed now, print its path and the pointer position | 0 |
| `move SESSION X Y` | glide the pointer to X,Y | 0 |
| `click SESSION X Y [--right] [--double]` | glide there and click | 0 |
| `drag SESSION X1 Y1 X2 Y2` | press at X1,Y1, glide to X2,Y2, release | 0 |
| `key SESSION KEY...` | press keys, xdotool names (`ctrl+s`, `Return`, `shift+a`) | 0 |
| `type SESSION 'text'` | type at human pace | 0 |
| `wait SESSION [--quiet 1] [--timeout 60]` | wait until the picture holds still for `--quiet` seconds | 0, 1 timed out |
| `cut`, `film`, `stop SESSION OUTDIR`, `render SESSION OUTDIR` | as in `browse` | 0 |

Every action takes `--say`, as in `browse`. Coordinates are pixels of what
is filmed, so a point read off a `shot` is the point to click; with
`--window` they are relative to the window. The app is the agent's to
start (on the display, in the container); `desk` films and drives it.

**Hands** are one `xdotool` process per action, its commands chained: a
glide is 15 moves 25 ms apart, and a click hovers 200 ms, then holds the
button 120 ms, as a person's does; some toolkits drop a click that arrives
with the pointer or is released at once. **The camera** is `ffmpeg -f x11grab
-draw_mouse 1` at 10 fps writing PNGs to stdout (`image2pipe`,
`-flush_packets 1`); the recorder splits the stream at each PNG's `IEND`,
stamps each picture when it arrives, and puts it on a `film.Reel`, which
drops repeats. Both run through the wrapper, so with `-- docker exec
CONTAINER` the display, app, xdotool, and ffmpeg live in the container and
the recording on the host. **Settling** reads the recorder's own log: an
action is over once no new picture has arrived for 400 ms since it ended,
at most 5 s. Takes, marks, beats, and rendering are `film.Set` and
`film.RenderPictures`, shared with `browse`.

## Instructions in the skill

These routes get clear instructions and snippets instead of code, each
ending in a `frames` directory, image, or clip that `build` takes:

- **Browser apps**: Playwright (or raw CDP) against a copy of the data, with
  the cursor overlay injected on every page, typing at human pace, and
  either Playwright's own video recording or screenshots raced against a
  short timeout. The existing cursor snippet and pacing rules carry over.
- **Desktop windows**: one ffmpeg capture line per OS (`avfoundation`,
  `gdigrab` by window title, `x11grab`), then look at a still before
  filming, because a blocked capture "succeeds" with wallpaper.
- **Log reels**: as today, for when capture is blocked or the thing to prove
  is a run.
- **Stills**: as today, as `image` scenes.
- **Native Windows terminals**: use WSL and `movie term`.

## Testing

- Go `testing`, no mocks.
- Unit tests for the pure functions listed under "Layout".
- Black-box tests run the binary on movies generated at test time with
  ffmpeg's lavfi sources, ported from today's `test_checker`,
  `test_assembly`, `test_paths`, `test_subtitles`, the cue-allocation and
  SRT cases in `test_subtitle_contract`, the checker case in
  `test_narration_contract`, and the `openai-chat` gate cases in
  `test_narration_contract` and `test_narration`; plus an unnarrated build,
  a frames scene whose visuals outlast its narration, card text with an
  undrawable character, and awkward paths (spaces, quotes, `%`, brackets,
  non-ASCII) for every scene kind.
- `term` tests drive a real tmux session through the verbs: a failing
  command, a timeout returning 3, `run` refusing while a command runs,
  `film off`/`on` splitting takes, `screen` text, `render` after the
  session is gone, refusal of non-empty directories, the user's history
  staying untouched, and a pass-through wrapper (`env`) standing in for
  `docker exec`.
- Rendering tests replay committed recordings and compare with committed
  PNGs within a small per-pixel tolerance, because Go's font rasterizing
  differs slightly between arm64 and amd64.
- The launcher is tested by running `bin/movie` on each CI platform,
  including Git Bash on Windows.
- Tests needing tmux, Piper, or a key skip with a message naming what is
  missing. CI installs tmux and Piper and fails if it cannot, so those tests
  never skip there.
- CI on macOS, Linux, and Windows (`build` and `check` only on Windows).

Python tests are deleted with the code they cover, after their real
behaviors are carried into Go tests; the mock-world tests are not ported.

## Order

Every step that deletes a Python tool rewrites the docs that mention it in
the same change, so the skill is never broken between steps. See the
implementation plan for the steps.

## Known losses

- No per-stage commands; rerunning any part reruns `build` (narration is
  cached).
- No phonetic respelling separate from the subtitles.
- Cards and subtitles: no CJK or emoji; cards reject what they cannot draw.
- `movie` scenes fill the frame; no inset or gain control.
- No subtitle styling or cue-length options.
- Rejected narration attempts are not kept on disk.
- Terminal: bash only, always clean (no user startup files or history), no
  native Windows (use WSL), no zsh, no ssh; CJK and emoji render as
  replacement boxes with a warning.
- No tool for browser apps or desktop capture; agents follow the
  instructions.

## Risks

- `capture-pane` polling rate through `docker exec`; the spike measures it.
- Piper's CLI is Python-packaged, so the keyless voice needs uv once.
- Five binaries, roughly 10 MB each (more with fonts), per release that
  changes Go source.
