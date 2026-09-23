# `movie`: a native rewrite of the proving-it-works tools

Date: 2026-09-22
Status: design, revised after adversarial review, awaiting review

## Why

The skill's knowledge is good and its code is not. The pipeline is five
Python scripts the agent must run in order with matching flags, handing state
through three files (`manifest.json`, `offsets.json`, `segments/`) that each
tool reads its own way. There is no shared core, the scene file is never
validated up front, the browser motion route has no tool at all, the terminal
route is a prose recipe on Unix and a 632-line daemon on Windows, and a third
of the docs are PowerShell and Git Bash copies of the pipeline because `uv`
shebangs do not run on Windows. The contract tests patch module internals by
name, so they cannot guard a restructuring.

## Decisions already made

| Decision | Choice |
|---|---|
| Language | Go. The work is orchestration (ffmpeg, HTTP, YAML, Chrome), and chromedp plus easy cross-compilation to Windows decide it. |
| Distribution | Prebuilt binaries committed to the repo. The skill never downloads executables at run time. |
| Speech recognition | Removed entirely. No ASR in the tool or in the skill's instructions. |
| `openai-chat` engine | Kept, gated by its own returned transcript. |
| Scene file | Redesigned freely. No backward compatibility code. |
| Terminal recording | The binary owns the pty and serves its own xterm.js page. Gated on a Windows ConPTY spike; the fallback is porting today's ttyd route. |
| `browse` | In scope, built last. |

## Shape

One binary, `movie`, invoked by its path inside the skill, never from PATH.

| Command | Does | Replaces |
|---|---|---|
| `movie build SCENES.yaml` | every stage below, in order; one exit code | the five scripts, run by hand |
| `movie narrate / assemble / subtitles / burn SCENES.yaml` | one stage, for debugging; see "Stages" | the same scripts |
| `movie check MOVIE` | the gate, usable on any movie | `check-movie` |
| `movie term serve / wait / run / key / watch / close` | film a real shell | both `film-terminal.py` files and the tmux recipe |
| `movie browse SCENES.yaml` | film a web app from scripted actions | nothing (new) |
| `movie version` | the source hash the binary was built from | nothing |

### Exit codes, identical for every command

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | the verdict is negative: movie not shippable, narration rejected, filmed command failed |
| 2 | usage or environment error: bad scene file, missing ffmpeg, unreadable input, session not ready |
| 3 | `term` only: the filmed command is still running |

### Repository layout

```
cmd/movie/                  main: flag parsing and dispatch only
internal/scene/             scene file types, parsing, validation
internal/state/             build.json: read, atomic write, staleness rules
internal/ffmpeg/            one runner for ffmpeg/ffprobe; probe helpers; path escaping
internal/narrate/           engines, acceptance checks
internal/assemble/          segments and concat
internal/subtitles/         cue timing, SRT writing and parsing, burning
internal/check/             sampling and the verdict
internal/chrome/            finding Chrome/Chromium/Edge, headless launch, screenshots
internal/proc/              owned-process-tree cleanup (Unix descendants, Windows job objects)
internal/term/              pty session, control server, xterm.js page, frame scheduler
internal/browse/            action runner, cursor overlay
skills/proving-it-works-with-a-movie/bin/
    movie                   POSIX sh launcher for macOS, Linux, and Git Bash
    movie-darwin-arm64  movie-darwin-amd64  movie-linux-amd64
    movie-linux-arm64   movie-windows-amd64.exe
script/build-binaries       cross-compiles all five, reproducibly
```

Every decision is a pure function over plain data, with I/O at the edges:
the check verdict takes change and loudness series with timestamps; cue
timing takes text and durations; narration acceptance takes script text, a
claimed transcript, and a measured duration; the terminal frame scheduler
takes capture timestamps and returns which frame fills each slot. These are
what the unit tests exercise, with no mocks.

### Launching

`bin/movie` is a POSIX sh script that maps `uname -s`/`uname -m` to a binary
(`Darwin`, `Linux`, and `MINGW*`/`MSYS*`/`CYGWIN*`, which select the Windows
`.exe`) and `exec`s it with `"$@"`, so arguments pass through untouched.
There is no `.cmd` launcher: cmd.exe mangles `%`, `&`, `|`, and quotes in
arguments, and `term run` passes arbitrary shell text. PowerShell calls the
`.exe` directly (`& "$skill/bin/movie-windows-amd64.exe"`), and SKILL.md shows
both forms once.

### Shipping the binaries

`script/build-binaries` builds all five with `CGO_ENABLED=0 -trimpath
-buildvcs=false -ldflags "-s -w -X main.sourceHash=<hash>"`, using the Go
toolchain pinned by the `toolchain` directive in `go.mod`. `<hash>` is a
SHA-256 over the git-tracked build inputs: `go.mod`, `go.sum`, every `.go`
file, and every embedded asset (the xterm.js copy, the card template, the
cursor overlay). `.gitattributes` forces LF for those inputs and marks
`bin/*` binary, so a Windows checkout hashes and builds identically.

The freshness test rebuilds all five into a temporary directory with the same
script and byte-compares them with the committed binaries. Any difference
fails, so the bytes that run are the bytes built from the reviewed source.
Binaries are rebuilt and committed only when build inputs change, in the same
commit.

### Prerequisites afterwards

ffmpeg and ffprobe; Chrome, Chromium, or Edge for cards, `term`, and `browse`;
Piper for the keyless voice. Piper installs once with
`uv tool install piper-tts` (which puts `piper` on PATH), and a voice with
`uvx --from piper-tts python -m piper.download_voices --data-dir DIR VOICE`.
The skill runs neither command itself. uv, Python, tmux, and ttyd are no
longer needed by the tool.

## The scene file

The kind of a scene is whichever one of `card`, `image`, `frames`, or `movie`
it has, so a scene cannot claim one kind and carry another's fields. Paths
are relative to the scene file.

```yaml
output: demo.mp4             # default: <scene file stem>.mp4
size: 1920x1080
fps: 30
voice:
  engine: auto               # auto | openai | openai-chat | piper
  name: nova
subtitles:
  font: DejaVu Sans
  size: 16
check:
  expect_audio: auto

scenes:
  - id: title
    card: {title: proving-it-works, subtitle: installed from a public marketplace}
    duration: 4

  - id: install
    frames: install/
    rate: 2.6
    narration: >-
      This is a container with nothing of ours in it.

  - id: sheet
    image: work/out/contact-sheet.png
    narration: So we do.

  - id: movie
    movie: work/out/counter.mp4
    height: 1000
    gain_db: 1.5
```

### Every field

Top level:

| Field | Type | Default | Meaning |
|---|---|---|---|
| `output` | path | `<scene stem>.mp4` | the finished movie |
| `size` | `WxH` | `1920x1080` | output resolution |
| `fps` | positive int | 30 | output frame rate |
| `voice.engine` | enum | `auto` | `auto` picks `openai` with a key, else `piper` |
| `voice.name` | string | `nova` (openai engines), `en_US-lessac-medium` (piper) | |
| `subtitles.font` | string | `DejaVu Sans` | burned subtitle font |
| `subtitles.size` | positive int | 16 | burned subtitle size |
| `subtitles.margin` | non-negative int | 30 | bottom margin, px |
| `subtitles.soft` | bool | false | embed a soft track even when burning is possible |
| `subtitles.max_chars` | positive int | 84 | cue length limit |
| `subtitles.max_secs` | positive number | 5.5 | cue duration limit |
| `check.expect_audio` | `auto`, bool | `auto` | see "`check` inside `build`" |
| `check.expect_subtitles` | `auto`, bool | `auto` | same |

Per scene:

| Field | Kinds | Default | Meaning |
|---|---|---|---|
| `id` | all | required | `[a-z0-9][a-z0-9-]*`, unique |
| `card.title`, `card.subtitle` | card | empty | HTML-escaped text |
| `card.background` | card | `#101014` | CSS color |
| `card.title_size`, `card.subtitle_size` | card | scaled from height | px |
| `image` | image | | path to a still |
| `frames` | frames | | directory of PNGs, lexical order |
| `rate` | frames | the movie's `fps` | playback rate of those PNGs |
| `movie` | movie | | path to a clip, played with its own audio |
| `height` | movie | 82% of output height | inner height; the rest is padding |
| `gain_db` | movie | 0 | audio gain |
| `duration` | card, image | 3 | minimum hold, seconds |
| `narration` | card, image, frames | none | the words spoken over the scene |

Validation happens before any work starts and reports every problem at once,
each with its scene id: unknown keys, a scene with zero or two kinds, a field
used on a kind that does not take it (including `narration` on a `movie`
scene), duplicate or malformed ids, missing source files, empty frame
directories, and non-positive sizes, rates, or durations.

## Stages and state

`movie build demo.yaml` writes the movie to `output`, and names every
artifact after the output, beside it:

```
demo.mp4                     the movie
demo.srt                     sidecar subtitles, which check reads
demo-check/contact-sheet.png the sheet to look at
demo-check/check.json        the full check result
```

and keeps its scratch beside the scene file, gitignored and safe to delete:

```
demo.build/
    build.json               the single record of this build
    narration/<id>.wav       accepted clips
    narration/.<id>.attempt-<random>.wav   rejected attempts, kept as evidence
    segments/<id>.mp4
    cut.mp4                  the assembled movie before subtitles
```

`build.json` replaces `manifest.json` and `offsets.json`. It is the only
channel between stages, so a stage run alone behaves exactly as it does
inside `build`:

| Stage | Reads | Writes |
|---|---|---|
| `narrate` | scene file, `build.json` | `narration/`, narration entries |
| `assemble` | scene file, `build.json` | `segments/`, `cut.mp4`, segment durations and offsets |
| `subtitles` | scene file, `build.json` | `<output stem>.srt` |
| `burn` | `cut.mp4`, the `.srt` | `output` |
| `check` | `output`, the `.srt`, `build.json` | `<output stem>-check/` |

`build.json` is always written atomically (temp file, then rename). Per
narrated scene it records the text, synthesis identity (engine, voice,
model), WAV name relative to `narration/`, and measured duration. Per scene
it records segment duration and offset in the cut.

**Staleness is checked by every reader.** `assemble` and `subtitles` refuse
to run when a narrated scene has no entry, when the entry's text differs from
the scene's narration after collapsing whitespace, or when its WAV is missing
or its path is absolute. This is today's `narration_contract` rule, applied
uniformly; today `make-subtitles` bypasses it.

**Narration acceptance order**, so an interrupted run can never leave an
entry pointing at unaccepted bytes:

1. Synthesize into a new `.<id>.attempt-<random>.wav`.
2. Run the acceptance checks on it. A rejected attempt stays on disk.
3. Remove the scene's entry from `build.json` (atomic write).
4. Rename the attempt to `<id>.wav`.
5. Measure it and write the new entry (atomic write).

A clip is reused only when its entry's text and synthesis identity match and
its WAV exists. `--renarrate ID...` (on `build` and `narrate`) forces new
clips for those scenes; `--renarrate all` forces every one.

Stages in `build`, stopping at the first failure:

1. **validate** the scene file.
2. **narrate**.
3. **assemble**: each segment lasts max(narration, visuals); short video
   freezes its last frame, short audio pads with silence; `movie` scenes keep
   their own audio and get a silent track when they have none. Only segments
   are rebuilt each time; narration is the only cache.
4. **subtitles**: cues timed within each narrated scene's measured interval
   at its offset in the cut, with today's millisecond allocation and
   rebalancing.
5. **burn** into the picture when ffmpeg has the `subtitles` filter and
   `subtitles.soft` is false. If the filter is missing, or the burn fails,
   embed a soft track instead. Either fallback prints a `WARN` line naming the
   reason, and `build` still exits 0, because the movie is valid; the warning
   is repeated in `build`'s final summary.
6. **check** the finished movie.

## Narration

Engines implement one interface: synthesize text to a WAV, optionally
returning the engine's own transcript.

| Engine | How | Model |
|---|---|---|
| `openai` | `/v1/audio/speech` | `gpt-4o-mini-tts` |
| `openai-chat` | `/v1/chat/completions` with audio output | `gpt-audio-1.5` |
| `piper` | runs `piper -m VOICE --data-dir DIR -f OUT.wav` | the voice name |

The key comes from `OPENAI_API_KEY`, then `llm keys get openai`, as today.
Piper voices live in `PIPER_VOICE_DIR`, defaulting to
`~/.cache/piper-voices`, as today; the e2e image relies on that variable. A
missing `piper` or a missing voice fails with the exact install command from
"Prerequisites".

Acceptance checks, all pure functions:

- **Transcript, `openai-chat` only.** This is today's gate, kept exactly. The
  transcript is the engine's own text, not a noisy ASR, so strict comparison
  is right: after normalizing (NFKC, casefold, letters/numbers/marks only),
  the transcript must contain speech, and the positional drift
  `|len(want) - len(got)| + mismatches at the same position` must not exceed
  `max(2, len(want) // 25)`. That rejects a one-word spoken preamble. A script
  that cannot be split into words (CJK, Thai) is rejected for this engine.
  The docs state plainly that this proves what the model says it said, not
  what is in the WAV.
- **Pace, every engine.** A clip of five or more words is rejected when its
  words per second falls outside 1.0 to 5.0. This catches empty, truncated,
  or runaway clips. It does not catch a skipped sentence or a short preamble,
  and the docs say so: for `openai` and `piper`, listening to the clips is
  the content check. Scripts without spaces skip this check and the output
  says so.

A new clip gets two attempts. A reused clip was accepted when it was made and
is not re-checked.

## `check`

### Sampling

The picture is sampled at 5 Hz: ffmpeg pipes 320-pixel-wide grey frames into
memory. The sample at time `t` counts as a change when more than 0.2% of its
pixels differ by more than 8 grey levels from the sample at `t - 1s`, today's
metric compared against one second earlier, so today's thresholds keep their
meaning while any beat held longer than 0.2 s becomes visible. Audio is
RMS over 1-second windows on a 0.2-second hop; a window at -45 dBFS or
louder is speech. (Today uses non-overlapping 1-second windows; the hop is
new.)

All verdict arithmetic is in seconds, not sample indices:

- `last_change` = time of the last changed sample; `last_talk` = end of the
  last speech window.
- **Front-loaded**: `last_change < 0.40 × duration` and speech continues more
  than 5 s past `last_change`.
- **Frozen tail warning**: speech continues more than 15 s past `last_change`.
- **Mid-movie hold warning**: more than 30 s between consecutive change
  times.
- **Not a movie**: duration under 1 s, or no video stream.
- **Never changes**: no changed sample.
- **No audio**: audio expected and no audio stream.
- **Silent**: audio expected, present, and no speech window.
- **No subtitles**: subtitles expected and none found, or none with cues.
- **Subtitles short**: last cue ends more than 3 s before `last_talk`.

The JSON reports `change_times` and `speech_windows` in seconds.

The contact sheet comes from a separate decode of 12 colour frames at evenly
spaced times, so it stays in colour, laid out with today's grid rule (4, 3,
5, or 2 columns, whichever divides the count).

### Flags

`movie check MOVIE [--out DIR] [--subs FILE] [--no-expect-audio]
[--no-expect-subtitles] [--json]`, as today. Subtitles are found at `--subs`,
else `<movie stem>.srt` beside the movie, else an embedded track.

### `check` inside `build`

`build` knows what the movie should contain, so with `auto` it sets
expectations from the scene file instead of guessing:

- Audio is expected when any scene is narrated or is a `movie` scene whose
  clip has an audio stream. An unnarrated reel of cards and frames is
  therefore checked as silent and passes.
- Subtitles are expected when any scene is narrated, and the "subtitles
  short" comparison uses the end of the last narrated scene from
  `build.json`, not detected speech, so speech inside a `movie` scene (which
  carries its own subtitles) does not fail the build.

`check.expect_audio` and `check.expect_subtitles` override `auto` with an
explicit true or false.

## `term`

### Spike first

The Windows ConPTY spike comes first. On a real Windows machine, with go-pty,
it must show that we can: spawn PowerShell 5.1, PowerShell 7, and Git Bash;
install the status prompt at launch; write keys and read output; resize; and
whether Git Bash still loses the first keystroke of a session. If the pty
route fails, this section is replaced by a port of today's ttyd route and the
spec is revised before the plan.

### Session

`movie term serve SESSION --shell bash|zsh|powershell|pwsh|gitbash
[--cwd DIR] [--profile] [--browser PATH] [--size WxH] [--font-size N]
[-- WRAPPER...]`

runs in the foreground, meant to be started as a background task the harness
keeps alive, as today. It:

- **Finds the shell.** `gitbash` looks under `Program Files\Git\bin\bash.exe`
  first so it never picks WSL's `bash.exe` from PATH.
- **Spawns it in a pty** with the status prompt installed at launch, so
  nothing is typed on camera to set it up: an rc file via `--rcfile` for
  bash, a `ZDOTDIR` for zsh, and `-NoExit -EncodedCommand` for PowerShell
  (base64, so cwd paths with quotes, `&`, brackets, or non-ASCII need no
  quoting). The prompt reports sequence number, success flag, exit code, and
  cwd in an OSC title sequence (BEL- or ST-terminated), which the page never
  shows.
- **Starts clean by default.** Without `--profile` the shell skips the user's
  startup files (`--noprofile --norc`, an empty `ZDOTDIR`, `-NoProfile`), so
  a filmed session is reproducible. `--profile` sources the user's normal
  startup files before installing the prompt, for tools that live on a PATH
  set there.
- **Wraps when asked.** `-- WRAPPER...` runs the shell through a command such
  as `docker exec -it -e PROMPT_COMMAND CONTAINER bash` or
  `ssh -t HOST bash`, so the binary and Chrome stay on the host and nothing
  of ours enters the container. For a wrapped bash, the prompt travels in the
  `PROMPT_COMMAND` environment variable, which the wrapper must forward; the
  docs show the exact forms for docker and ssh. The spike confirms this on
  the e2e image.
- **Verifies the cwd** reported by the first prompt matches `--cwd` (by leaf
  name for Git Bash's `/c/...` paths), as today.
- **Serves the page** on 127.0.0.1: a vendored xterm.js (MIT), fed from the
  pty over a websocket. The page URL and websocket both require a random
  token and check the Origin; the socket is output-only, and xterm.js runs
  with input disabled, so no other page can read or type into the shell.
- **Drives Chrome** (headless, software GL) through chromedp to screenshot
  the page, and runs today's preflight: print a dense line, refuse a blank
  canvas, save `ready.png`.
- **Opens a control endpoint** on 127.0.0.1 guarded by the same token. Only
  after the preflight passes does it write `SESSION/session.json`
  atomically (endpoint address and token) and print one JSON line
  `{"ready": true, ...}`. The existence of `session.json` plus a successful
  ping of the endpoint is readiness; there are no `ready`, `stop`, or `mark`
  files.
- **Logs** raw pty output to `SESSION/terminal.log`.

`movie term wait SESSION [--seconds 60]` blocks until the session is ready,
replacing the polling loops the docs currently show in PowerShell and Bash.
`run`, `key`, and `watch` exit 2 at once if it is not ready.

### Filming

`run SESSION 'cmd'` refuses unless the shell is at a prompt, so keys cannot
land in a running program's stdin. It writes the command and Enter to the pty
directly, then films at 5 fps into `--record DIR` until the next prompt plus a
1.5-second hold, or `--seconds`. `key` writes one key's bytes. `watch` films
without input, waiting for the prompt after the last `run` or `key`. Each
prints JSON with `outcome`, `ok`, `exit_code`, `cwd`, and frame count, and
writes `take.json` (including a ready-to-paste `frames` scene) beside the
frames. Record and session directories must be new or empty, checked before
any input is sent.

Frame timing is today's rule, as a pure function over capture timestamps:
frames land on the 5 fps grid, a slow capture repeats the previous frame into
missed slots, and the take ends exactly at its endpoint.

### Cleanup

`close` asks the daemon to exit and waits up to 30 s for its confirmation.
The daemon kills the whole process tree of everything it started, and
nothing else: on Unix it walks descendants of the shell and Chrome (a pty
child starts its own session, so a process group is not enough); on Windows
each child is assigned to a job object at start, and closing the job kills
its tree. It never kills PIDs read back from files. `close` exits 1 when
cleanup cannot be confirmed.

## `browse`

Built last, and specified in its own design before its plan. The outline: a
scene file lists actions (`goto`, `wait_for`, `click`, `type`, `append`,
`select`, `pause`); the recorder drives Chrome through chromedp with the
cursor overlay built in, types at human pace, races every screenshot against
a short timeout, and writes a frames directory `build` can use.

## Chrome discovery

Shared by cards, `term`, and `browse`: an explicit `--browser` wins and fails
loudly if unusable; otherwise search Chrome, Chromium (`chromium`,
`chromium-browser`), and Edge, including the Windows install locations under
`LOCALAPPDATA`, `PROGRAMFILES`, `PROGRAMFILES(X86)`, and `PROGRAMW6432`, as
`browser_tools.py` does today. `build`, `assemble`, and `term serve` take
`--browser`.

## Testing

- Go `testing`, no mocks anywhere.
- **Unit tests** for every pure function: check verdicts from synthetic
  series, cue timing, scene validation, narration acceptance (including a
  one-word preamble and a dropped clause for `openai-chat`), status-prompt
  parsing (BEL and ST terminators, semicolons in cwd), the frame scheduler,
  and Chrome candidate ordering per platform.
- **Black-box tests** run the built binary against movies generated at test
  time from ffmpeg's lavfi sources, ported from today's real-media suites
  (`test_assembly`, `test_checker`, `test_paths`, and the real-media parts of
  `test_subtitles`, including the burn-failure fallback). Paths with spaces,
  quotes, percent signs, brackets, and non-ASCII characters stay covered.
- **Card rendering**: a real browser renders a card at an awkward path, and a
  render timeout kills only the processes it started.
- **`term`** tests run a real pty and real headless Chrome: run, key, watch,
  still-running, failed commands, and close killing the shell tree while
  sparing an unrelated process. `MOVIE_TEST_SHELL` selects the shell, as
  today.
- **The freshness test** under "Shipping".
- **Narration** tests that need the network or Piper run when a key or
  `piper` is present.
- **Skips are loud and can be made fatal.** Every skip names what is missing.
  `MOVIE_REQUIRE=chrome,piper,openai,...` turns the named skips into
  failures, replacing `run-tests.py --require-capabilities`.
- **CI**: a GitHub Actions matrix on macOS, Linux, and Windows, with the
  Windows job running the `term` tests under PowerShell 5.1, PowerShell 7,
  and Git Bash.

The Python tests are deleted as their code is replaced. Each real behavior
they assert is carried into a unit or black-box test first; tests that only
exercise fake processes, fake CDP, or patched internals
(`test_recorder_contract.py`, most of `test_narration_contract.py`,
`test_subtitle_contract.py`, and `test_narration.py`) are not ported as such.

## Sub-projects and order

Each gets its own implementation plan and deletes the Python it replaces in
the same change.

1. **Core and `check`**: Go module, `.gitattributes`, launcher,
   `script/build-binaries`, freshness test, CI, `movie version`,
   `movie check`. Deletes `check-movie` and its tests; SKILL.md points at
   `movie check`.
2. **`build`**: scene file, state, narrate, assemble, subtitles, burn, build,
   Chrome discovery for cards. Deletes the remaining scripts, their helper
   modules and tests, the ASR docs, and the Windows pipeline sections;
   converts `examples/e2e/scenes.yaml`.
3. **`term`**: ConPTY spike, then the recorder. Deletes both
   `film-terminal.py` files and their tests; rewrites
   `recording-a-terminal.md` and moves the e2e demo to a wrapped
   `docker exec` session.
4. **`browse`**: its own design first.

## Risks

- **ConPTY behavior on Windows** is the largest unknown; the spike decides it.
- **Wrapped shells** depend on the wrapper forwarding the prompt variable;
  the spike checks docker, and the docs cover ssh.
- **Piper's CLI** is Python-packaged, so the keyless voice needs uv to install
  it once. The skill does not do that install itself.
- **Repo size** grows by five binaries (on the order of 10 MB each) per
  release that changes build inputs.
- **The pace range** is a guess until tuned against real clips, and it is a
  coarse check by design.
