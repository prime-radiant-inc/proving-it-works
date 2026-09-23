# `movie`: a native rewrite of the proving-it-works tools

Date: 2026-09-22
Status: design, awaiting review

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
| Language | Go. The work is orchestration (ffmpeg, HTTP, YAML, Chrome), and chromedp plus trivial cross-compilation to Windows decide it. |
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
| `movie build SCENES.yaml` | narrate, assemble, subtitle, burn, check; one exit code | the five scripts, run by hand |
| `movie narrate / assemble / subtitles / burn SCENES.yaml` | one stage of `build`, for debugging | the same scripts |
| `movie check MOVIE` | the gate, usable on any movie | `check-movie` |
| `movie term serve / run / key / watch / close` | film a real shell | both `film-terminal.py` files and the tmux recipe |
| `movie browse SCENES.yaml` | film a web app from scripted actions | nothing (new) |
| `movie version` | the source hash the binary was built from | nothing |

### Exit codes, identical for every command

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | the verdict is negative: movie not shippable, narration rejected, filmed command failed |
| 2 | usage or environment error: bad scene file, missing ffmpeg, unreadable input |
| 3 | `term` only: the filmed command is still running |

### Repository layout

```
cmd/movie/                  main: flag parsing and dispatch only
internal/scene/             scene file types, parsing, validation
internal/ffmpeg/            one runner for ffmpeg/ffprobe; probe helpers
internal/narrate/           engines, acceptance checks, narration cache
internal/assemble/          segments and concat
internal/subtitles/         cue timing, SRT writing and parsing, burning
internal/check/             sampling and the verdict
internal/term/              pty session, control server, xterm.js page
internal/browse/            action runner, cursor overlay
internal/chrome/            finding Chrome/Edge, headless launch, screenshots
skills/proving-it-works-with-a-movie/bin/
    movie                   POSIX sh launcher: picks the binary by uname
    movie.cmd               Windows launcher
    movie-darwin-arm64  movie-darwin-amd64  movie-linux-amd64
    movie-linux-arm64   movie-windows-amd64.exe
script/build-binaries       cross-compiles all five, reproducibly
```

Every decision is a pure function over plain data, with I/O at the edges:
the check verdict takes per-sample change and loudness series; cue timing
takes text and durations; the narration acceptance check takes script text,
a claimed transcript, and a measured duration. These are what the unit tests
exercise, with no mocks.

### Shipping the binaries

`script/build-binaries` builds with `-trimpath -ldflags "-s -w"` and stamps
each binary with a hash of the Go source tree. A test rebuilds that hash from
the working tree and fails if any committed binary reports a different one,
so the bytes that run are the bytes that were reviewed. Binaries are rebuilt
and committed only when Go source changes, in the same commit.

### Prerequisites afterwards

ffmpeg and ffprobe; Chrome or Edge for cards, `term`, and `browse`; Piper for
the keyless voice (`uv tool install piper-tts` puts `piper` on PATH). uv,
Python, tmux, and ttyd are no longer needed by the tool.

## The scene file

The kind of a scene is whichever one of `card`, `image`, `frames`, or `movie`
it has, so a scene cannot claim one kind and carry another's fields. Paths are
relative to the scene file. Build settings that were CLI flags move into the
file, because they belong to the movie.

```yaml
output: demo.mp4             # default: <scene file stem>.mp4
size: 1920x1080              # default 1920x1080
fps: 30                      # default 30
voice:
  engine: auto               # auto | openai | openai-chat | piper
  name: nova                 # default: nova for openai*, en_US-lessac-medium for piper

scenes:
  - id: title
    card: {title: proving-it-works, subtitle: installed from a public marketplace}
    duration: 4

  - id: install
    frames: install/         # directory of PNGs, in lexical order
    rate: 2.6
    narration: >-
      This is a container with nothing of ours in it.

  - id: sheet
    image: work/out/contact-sheet.png
    narration: So we do.

  - id: movie
    movie: work/out/counter.mp4
    height: 1000             # inner height; the rest is padding
    gain_db: 1.5
```

Validation happens before any work starts and reports every problem at once,
each with its scene id: unknown keys, a scene with zero or two kinds,
duplicate or malformed ids (`[a-z0-9][a-z0-9-]*`), missing source files or
empty frame directories, non-positive `duration`/`rate`/`fps`, `narration` on
a `movie` scene (today it is silently ignored), and a `card` or `image` scene
with neither `duration` nor `narration`.

Card titles and subtitles are HTML-escaped (today an `&` or `<` breaks the
card).

## `build` and its state

`movie build demo.yaml` writes, beside the scene file:

```
demo.mp4                     the movie
demo.srt                     sidecar subtitles, which check reads
demo-check/contact-sheet.png the sheet to look at
demo.build/                  scratch, safe to delete, gitignored
    build.json               the single record of this build
    narration/<id>.wav       accepted clips
    narration/.<id>.attempt-<n>.wav   rejected clips, kept as evidence
    segments/<id>.mp4
```

`build.json` replaces `manifest.json` and `offsets.json`. Per scene it holds
the narration text, synthesis identity (engine, voice, model), accepted WAV,
measured narration duration, segment duration, and offset in the cut. Stages
pass data in memory; the file exists so a failed build can be inspected and so
narration can be reused. It is written atomically, and a narration entry is
only written after its clip is accepted.

Stages, in order, stopping at the first failure:

1. **validate** the scene file.
2. **narrate** every narrated scene, reusing a clip only when its text and
   synthesis identity are unchanged.
3. **assemble**: each segment lasts max(narration, visuals); short video
   freezes its last frame, short audio pads with silence; `movie` scenes keep
   their own audio, and get a silent track when they have none.
4. **subtitles**: cues timed within each narrated scene's measured interval at
   its offset in the cut. No offsets file, no ordering mistakes.
5. **burn** into the picture when ffmpeg has the `subtitles` filter; otherwise
   embed a soft track and say so loudly, in the build output and again in
   check's output.
6. **check** the finished movie.

Only narration is cached. Segments are rebuilt every time.

## Narration

Engines implement one interface: synthesize text to a WAV, optionally
returning the engine's own transcript.

| Engine | How | Model |
|---|---|---|
| `openai` | `/v1/audio/speech` | `gpt-4o-mini-tts` |
| `openai-chat` | `/v1/chat/completions` with audio output | `gpt-audio-1.5` |
| `piper` | runs the `piper` CLI | the voice name |

`auto` picks `openai` when a key exists and `piper` otherwise. The key comes
from `OPENAI_API_KEY`, then `llm keys get openai`, as today.

Acceptance checks, all pure functions:

- **Pace**, every engine: a clip of five or more words whose words-per-second
  falls outside a plausible range is rejected. This catches empty clips and
  skipped sentences. The starting range is 1.0 to 5.0 words per second, to be
  tuned against real clips in the build sub-project. Scripts that are not
  space-delimited (CJK, Thai) skip this check and the output says so.
- **Transcript**, `openai-chat` only: the returned transcript must contain
  speech and pass the structural comparison (length change at most 15% and no
  run of four or more missing, invented, or changed words). A script that
  cannot be compared word by word is rejected for this engine. The docs state
  plainly that this proves what the model says it said, not what is in the WAV.

A new clip gets two attempts; rejected attempts stay on disk as evidence. A
reused clip was accepted when it was made and is not re-checked.

A missing Piper voice fails with the exact command to install it. The tool
itself downloads nothing. The exact voice-install command is confirmed in the
build sub-project's plan.

## `check`

Behavior is preserved, with one improvement: it samples at 5 Hz instead of
1 Hz. ffmpeg decodes the movie to 320-pixel-wide grey frames piped into
memory (no PNG files). Each sample is compared with the sample one second
earlier, using today's metric: the fraction of pixels whose grey value moved
by more than 8, with more than 0.2% counting as a new state. Comparing
against one second back keeps the tuned thresholds meaningful while any beat
held longer than 0.2 seconds becomes visible, so the "hold every beat longer
than a second" rule shrinks accordingly. Audio is windowed RMS as today
(1-second windows, 0.2-second hop, speech at -45 dBFS or louder).

Verdicts are today's: front-loaded action with narration over a frozen
picture, a picture that never changes, a silent audio track, missing
narration, and missing or short subtitles (sidecar `.srt` first, then an
embedded track). Warnings are today's too. The contact sheet keeps its
grid-filling layout. `--json` writes the full result.

## `term`

The Windows ConPTY spike comes first. It proves, with go-pty on a real
Windows machine, that we can spawn PowerShell 5.1, PowerShell 7, and Git Bash,
install the status prompt at launch, write keys, read output, and resize. If
it fails, this section is replaced by a port of today's ttyd route and the
spec is revised before the plan.

`movie term serve SESSION --shell bash|zsh|powershell|pwsh|gitbash [--cwd DIR]`
runs a daemon that:

- spawns the shell in a pty with a status prompt installed at launch
  (`--rcfile` for bash, `ZDOTDIR` for zsh, `-Command` for PowerShell), so
  nothing is typed on camera to set it up. The prompt reports each command's
  sequence number, success flag, exit code, and cwd in an OSC title sequence,
  which the terminal page never shows.
- serves a page on 127.0.0.1 with a vendored copy of xterm.js (MIT), fed
  from the pty over a websocket.
- drives headless Chrome through chromedp to screenshot that page. Launch
  uses software GL; a preflight prints a dense line and refuses a blank
  canvas, as today.
- exposes a control endpoint on 127.0.0.1 guarded by a random token, both
  recorded in `SESSION/session.json`. The other verbs talk to it; there are
  no polled `ready`, `stop`, or `mark` files.
- appends raw pty output to `SESSION/terminal.log`.

`run 'cmd'` refuses unless the shell is at a prompt, so keys cannot land in a
running program's stdin. It writes the command and Enter to the pty
directly, then films at 5 fps into `--record DIR` until the next prompt plus
a 1.5-second hold, or `--seconds`. `key` writes one key's bytes. `watch`
films without input. Each prints JSON with `ok`, `exit_code`, `cwd`, and
frame count, and writes `take.json` beside the frames. A slow screenshot
repeats the previous frame so the take plays at exactly 5 fps. Record
directories and session directories must be new or empty.

`close` tells the daemon to exit; it kills only processes it started and
confirms cleanup, as today.

Filming a shell inside a container means running the Linux binary inside the
container. The end-to-end demo moves to that model and
`examples/film-terminal.py` is deleted.

## `browse`

Built last, and specified in its own design before its plan. The outline: a
scene file lists actions (`goto`, `wait_for`, `click`, `type`, `append`,
`select`, `pause`); the recorder drives Chrome through chromedp with the
cursor overlay built in, types at human pace, races every screenshot against
a short timeout, and writes a frames directory `build` can use.

## Testing

- Go `testing`, no mocks anywhere.
- Unit tests for every pure decision function: verdicts from synthetic
  series, cue timing, scene validation, pace and transcript acceptance,
  launcher platform selection.
- Black-box tests run the built binary against movies generated at test time
  from ffmpeg's lavfi sources, ported from today's real-media suites
  (`test_assembly`, `test_checker`, `test_paths`, the real-media parts of
  `test_subtitles`). Paths with spaces, quotes, percent signs, brackets, and
  non-ASCII characters stay covered.
- `term` tests run a real pty and real headless Chrome. When Chrome is absent
  they skip with a message naming what is missing.
- The binary freshness test described under Shipping.
- Narration engine tests that need the network or Piper run only when a key
  or `piper` is present, and say so when skipped.

The mock-world Python tests (`test_recorder_contract.py`, the patched parts
of `test_narration_contract.py`, `test_subtitle_contract.py`,
`test_narration.py`) are deleted as their code is replaced. Every real
behavior they assert is carried into a pure-function or black-box test first.

## Sub-projects and order

Each gets its own implementation plan and deletes the Python it replaces in
the same change.

1. **Core and `check`**: Go module, launchers, `script/build-binaries`,
   freshness test, `movie version`, `movie check`. Deletes `check-movie` and
   its tests; SKILL.md points at `movie check`.
2. **`build`**: scene file, narrate, assemble, subtitles, burn, build.
   Deletes the remaining scripts, their helper modules and tests, the
   ASR-related docs, and the Windows pipeline sections; converts
   `examples/e2e/scenes.yaml`.
3. **`term`**: ConPTY spike, then the recorder. Deletes both
   `film-terminal.py` files and their tests; rewrites
   `recording-a-terminal.md` and the e2e demo.
4. **`browse`**: its own design first.

## Risks

- **ConPTY behavior on Windows** is the largest unknown; the spike decides it.
- **Piper's CLI** is Python-packaged, so the keyless voice still needs uv or
  pip to install it once. The skill does not do that install itself.
- **Repo size** grows by roughly five binaries (on the order of 10 MB each)
  per release that changes Go source.
- **Pace thresholds** are a guess until tuned against real clips.
