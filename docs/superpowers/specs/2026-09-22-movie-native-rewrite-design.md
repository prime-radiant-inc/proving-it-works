# `movie`: a native rewrite of the proving-it-works tools

Date: 2026-09-22
Status: design, revised after two adversarial reviews, awaiting review

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
| 2 | usage or environment error: bad scene file, missing ffmpeg, unreadable input, stale build state, session not ready |
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
internal/proc/              owned-process-tree cleanup
internal/term/              pty session, control server, xterm.js page, keys, frame scheduler
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
claimed transcript, and a measured duration; key encoding takes a key name
and the terminal's cursor-key mode; the terminal frame scheduler takes
capture timestamps and returns which frame fills each slot. These are what
the unit tests exercise, with no mocks.

### Launching

`bin/movie` is a POSIX sh script that maps `uname -s`/`uname -m` to a binary
(`Darwin`, `Linux`, and `MINGW*`/`MSYS*`/`CYGWIN*`, which select the Windows
`.exe`) and `exec`s it with `"$@"`. On the Windows branch it first exports
`MSYS_NO_PATHCONV=1` and `MSYS2_ARG_CONV_EXCL='*'`, because Git Bash
otherwise rewrites Unix-looking arguments (`/usr/bin/env`, `/c/...`) when it
starts a native `.exe`, which would corrupt `term run` commands and wrapper
arguments. Paths passed from Git Bash must therefore be in Windows form
(`cygpath -m`), as the docs already say. The binary removes both variables
from any shell it films.

There is no `.cmd` launcher: cmd.exe mangles `%`, `&`, `|`, and quotes in
arguments, and `term run` passes arbitrary shell text. PowerShell calls the
`.exe` directly (`& "$skill/bin/movie-windows-amd64.exe"`), and SKILL.md shows
both forms once.

### Shipping the binaries

`script/build-binaries` reads the exact Go version from the `toolchain` line
in `go.mod` and builds with `GOTOOLCHAIN=<that version>` (the `toolchain`
directive alone is only a minimum), `GOAMD64=v1`, `GOARM64=v8.0`,
`CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, and
`-ldflags "-s -w -X main.sourceHash=<hash>"`.

`<hash>` is a SHA-256 over the git-tracked inputs that go into the binary:
`go.mod`, `go.sum`, every non-test `.go` file outside `testdata/`, and every
embedded asset (the xterm.js copy, the card template, the cursor overlay).
Tests are excluded, so editing a test never forces new binaries.
`.gitattributes` forces LF for those inputs and marks `bin/*` binary, so a
Windows checkout hashes and builds identically.

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
The skill runs neither command itself. Today `narrate` downloads a missing
voice on first use; that stops, and a missing voice is an error naming the
command. uv, Python, tmux, and ttyd are no longer needed by the tool.

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
    narration: So we check it with smevals.
    speak: So we check it with S M evals.

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
| `narration` | card, image, frames | none | the words the viewer reads in the subtitles |
| `speak` | card, image, frames | `narration` | what the voice is sent, for phonetic respellings; requires `narration` |

`speak` implements the existing advice in narrating.md to respell a
mispronounced term for the voice only: subtitles show `narration`; the voice,
the `openai-chat` transcript comparison, and the pace check use `speak`.

Validation happens before any work starts and reports every problem at once,
each with its scene id: unknown keys, a scene with zero or two kinds, a field
used on a kind that does not take it (including `narration` on a `movie`
scene), `speak` without `narration`, duplicate or malformed ids, missing
source files, empty frame directories, and non-positive sizes, rates, or
durations.

## Stages and state

`movie build demo.yaml` names every artifact after the output file's stem,
beside the output:

```
demo.mp4                     the movie
demo.srt                     sidecar subtitles, which check reads (narrated movies only)
demo-check/contact-sheet.png the sheet to look at
demo-check/check.json        the full check result
```

Scratch lives beside the scene file and is named after the scene file's
stem, gitignored and safe to delete:

```
demo.build/
    build.json               the single record of this build
    narration/<id>.wav       accepted clips
    narration/.<id>.attempt-<random>.wav   rejected attempts, kept as evidence
    segments/<id>.mp4
    cut.mp4                  the assembled movie before subtitles
```

`build.json` replaces `manifest.json` and `offsets.json`. It is the only
channel between the build stages, so a stage run alone behaves exactly as it
does inside `build`:

| Stage | Reads | Writes |
|---|---|---|
| `narrate` | scene file, `build.json` | `narration/`, narration entries |
| `assemble` | scene file, `build.json` | `segments/`, `cut.mp4`, the assembly record |
| `subtitles` | scene file, `build.json` | `<output stem>.srt`, the subtitles record |
| `burn` | scene file, `build.json`, `cut.mp4`, the `.srt` | `output` |

`build.json` is always written atomically (temp file, then rename). Per
narrated scene it records the text sent to the voice, synthesis identity
(engine, voice, model), WAV name relative to `narration/`, and measured
duration.

### Staleness

Every stage refuses stale input with exit 2 and a message naming the stage to
rerun:

- `assemble` and `subtitles` refuse when a narrated scene has no entry, when
  the entry's text differs from the scene's `speak` text after collapsing
  whitespace, when its synthesis identity differs from what the scene file
  now asks for, or when its WAV is missing or its path is absolute. This is
  today's `narration_contract` rule plus the identity check, applied
  uniformly; today `make-subtitles` bypasses it.
- `assemble` records a fingerprint: a hash of the scene file's bytes and the
  narration entries it used, along with each scene's segment duration and
  offset. `subtitles` refuses unless that fingerprint matches the current
  scene file and entries, and records the same fingerprint plus a hash of the
  `.srt` it wrote. `burn` refuses unless both records match the current
  inputs and the `.srt` on disk still has the recorded hash.

### Narration acceptance order

So an interrupted run can never leave an entry pointing at unaccepted bytes:

1. Synthesize into a new `.<id>.attempt-<random>.wav`.
2. Run the acceptance checks on it. A rejected attempt stays on disk.
3. Remove the scene's entry from `build.json` (atomic write).
4. Rename the attempt to `<id>.wav`.
5. Measure it and write the new entry (atomic write).

If every attempt is rejected, the old entry, if any, is left in place and the
stage exits 1; because the readers check text and synthesis identity against
the scene file, a stale entry left this way can never be assembled.

A clip is reused only when its entry's text and synthesis identity match and
its WAV exists. `--renarrate ID...` (on `build` and `narrate`) forces new
clips for those scenes; `--renarrate all` forces every one.

### Stages in `build`

In order, stopping at the first failure:

1. **validate** the scene file.
2. **narrate**.
3. **assemble**: each segment lasts max(narration, visuals); short video
   freezes its last frame, short audio pads with silence; `movie` scenes keep
   their own audio and get a silent track when they have none. Segments are
   rebuilt every time; narration is the only cache.
4. **subtitles**: cues timed within each narrated scene's measured interval
   at its offset in the cut, with today's millisecond allocation and
   rebalancing. Skipped when no scene is narrated: ffmpeg rejects an empty
   SRT for both burning and a soft track.
5. **burn** into the picture when ffmpeg has the `subtitles` filter and
   `subtitles.soft` is false. If the filter is missing, or the burn fails,
   embed a soft track instead. Either fallback prints a `WARN` line naming the
   reason, and `build` still exits 0, because the movie is valid; the warning
   is repeated in `build`'s final summary. With no narrated scene, `cut.mp4`
   is copied to `output` unchanged.
6. **check** the finished movie, with expectations set by `build` (below).

## Narration

Engines implement one interface: synthesize text to a WAV, optionally
returning the engine's own transcript.

| Engine | How | Model |
|---|---|---|
| `openai` | `/v1/audio/speech` | `gpt-4o-mini-tts` |
| `openai-chat` | `/v1/chat/completions` with audio output | `gpt-audio-1.5` |
| `piper` | runs `piper -m VOICE --data-dir DIR -i TEXT.txt -f OUT.wav` | the voice name |

Piper reads the text from a UTF-8 file (`-i`), never from arguments, where
narration words that look like flags would be misparsed, and never from
stdin, which Python decodes in the ANSI code page on Windows.

The key comes from `OPENAI_API_KEY`, then `llm keys get openai`, as today.
Piper voices live in `PIPER_VOICE_DIR`, defaulting to
`~/.cache/piper-voices`, as today; the e2e image relies on that variable. A
missing `piper` or a missing voice fails with the exact install command from
"Prerequisites".

Acceptance checks, all pure functions, applied to the `speak` text:

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
- **Not a movie**: duration under 1 s, or no video stream.
- **Front-loaded**: `last_change < 0.40 × duration` and speech continues more
  than 5 s past `last_change`.
- **Frozen tail warning**: speech continues more than 15 s past `last_change`.
- **Mid-movie hold warning**: more than 30 s between consecutive change
  times.
- **Never changes**: no changed sample.
- **No audio**: audio expected and no audio stream.
- **Silent**: audio expected, present, and no speech window.
- **No subtitles**: subtitles expected, speech present, and no subtitles
  found, or subtitles with no cues. As today, a movie with no detected
  speech never needs subtitles.
- **Subtitles short**: last cue ends more than 3 s before the speech end
  (`last_talk`, unless `build` supplies the narration end).

The JSON reports `change_times` and `speech_windows` in seconds.

The contact sheet comes from a separate decode of 12 colour frames at evenly
spaced times, so it stays in colour, laid out with today's grid rule (4, 3,
5, or 2 columns, whichever divides the count).

### Standalone flags

`movie check MOVIE [--out DIR] [--subs FILE] [--no-expect-audio]
[--no-expect-subtitles] [--json]`, as today. Subtitles are found at `--subs`,
else `<movie stem>.srt` beside the movie, else an embedded track. Standalone
`check` reads no build state; it judges the movie by what it can measure.

### `check` inside `build`

`build` calls the same check code in-process, passing expectations it
derives from the scene file and `build.json`, which standalone `check` cannot
know:

- **Audio expected** when any scene is narrated, or when any `movie` scene's
  clip contains a speech window by the rule above. A `movie` scene whose
  track is silent (every movie this tool builds has an audio track) does not
  count. An unnarrated reel of cards and frames is therefore checked as
  silent and passes.
- **Subtitles expected** when any scene is narrated. "Subtitles short"
  compares against the end of the last narrated scene from `build.json`, not
  detected speech, so speech inside a `movie` scene (which carries its own
  subtitles) does not fail the build.

`check.expect_audio` and `check.expect_subtitles` override `auto` with an
explicit true or false.

## `term`

### Spike first

The Windows ConPTY spike comes first. On a real Windows machine, with go-pty,
it must show that we can: spawn PowerShell 5.1, PowerShell 7, and Git Bash;
install the status prompt at launch; write input and read output; resize;
whether Git Bash still loses the first input of a session; and whether a
child can be kept inside a job object from its first instruction. It also
checks, on the e2e image, a wrapped `docker exec` session (below). If the pty
route fails, this section is replaced by a port of today's ttyd route and the
spec is revised before the plan.

### Session

`movie term serve SESSION --shell bash|zsh|powershell|pwsh|gitbash
[--shell-exe PATH] [--cwd DIR] [--profile] [--browser PATH] [--size WxH]
[--font-size N] [-- WRAPPER...]`

runs in the foreground, meant to be started as a background task the harness
keeps alive, as today. In order, it:

1. **Opens the page first.** It serves a page on 127.0.0.1 with a vendored
   xterm.js (MIT) at `--size` (default 1600×900) and `--font-size` (default
   17), launches Chrome (headless, software GL) through chromedp, loads the
   page, and reads the columns and rows xterm.js lays out at that size.
2. **Spawns the shell** in a pty of exactly those columns and rows, so the
   shell never believes in a different geometry than the picture shows.
3. **Relays both ways** over a websocket. Output flows to the page. Input
   from the page is forwarded to the pty, because xterm.js answers terminal
   queries (cursor position, device attributes, colours) through that path,
   and programs that ask (fish, crossterm/ratatui TUIs, many agent CLIs)
   stall without answers. No human types into the headless page, so in
   practice only those answers flow back. The page URL and websocket both
   require a random token and check the Origin, so no other page can read or
   type into the shell.
4. **Runs today's preflight**: print a dense line, refuse a blank canvas,
   save `ready.png`.
5. **Opens a control endpoint** on 127.0.0.1 guarded by the same token, then
   writes `SESSION/session.json` atomically (endpoint address and token) and
   prints one JSON line `{"ready": true, ...}`. The existence of
   `session.json` plus a successful ping of the endpoint is readiness; there
   are no `ready`, `stop`, or `mark` files.

Raw pty output is appended to `SESSION/terminal.log`.

**Finding the shell.** `--shell-exe` wins. Otherwise `gitbash` looks for
`Git\bin\bash.exe` under `ProgramFiles`, `ProgramW6432`, and
`LOCALAPPDATA\Programs`, and never searches PATH, so it cannot pick WSL's
`bash.exe`. Other shells are found on PATH.

**The status prompt** reports sequence number, success flag, exit code, and
cwd in an OSC title sequence (BEL- or ST-terminated), which the page never
shows. It is installed at launch, so nothing is typed on camera:

| Shell | Clean (default) | `--profile` |
|---|---|---|
| bash | `bash --noprofile --norc -i`, with the prompt in the `PROMPT_COMMAND` environment variable and `PS1` set | `bash --rcfile OURS -i`, where our rc sources `~/.bashrc` and then sets the prompt |
| zsh | `ZDOTDIR` pointing at our directory, whose `.zshenv` runs `unsetopt GLOBAL_RCS` (skipping `/etc/zshrc` and friends) and whose `.zshrc` installs a `precmd` hook | our `.zshrc` sources the user's `.zshrc` first |
| PowerShell | `-NoLogo -NoProfile -NoExit -EncodedCommand <base64 prompt script>` | same without `-NoProfile` |
| gitbash | as bash | as bash |

The bash prompt command captures `$?` first and then runs
`export -n PROMPT_COMMAND`, so after the first prompt it is no longer
exported and a nested bash cannot emit markers of its own. (Verified with
bash 3.2: status, environment `PS1`, and the unexport all behave under
`--noprofile --norc`.) `-EncodedCommand` means cwd paths with quotes, `&`,
brackets, or non-ASCII need no quoting. The filmed shell's environment drops
`TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `TERM_SESSION_ID`,
`MSYS_NO_PATHCONV`, and `MSYS2_ARG_CONV_EXCL`, which would otherwise make
macOS's session restore or the launcher's settings leak into the take. After
the first prompt, `serve` checks the reported cwd against `--cwd` (default:
the current directory; by leaf name for Git Bash's `/c/...` paths), as today.

**Wrapped shells**, for a container or a remote host, so the binary and
Chrome stay on the host and nothing of ours enters the target:
`-- docker exec -it CONTAINER` or `-- ssh -t HOST`. The wrapper is a prefix;
`serve` appends the target command itself:
`env PROMPT_COMMAND=<prompt> PS1=<ps1> bash --noprofile --norc -i`. For
`ssh`, which joins its arguments into one remote command line, `serve` passes
that command as a single shell-quoted argument. The prompt travels in the
command line, not through environment forwarding, so it needs no `AcceptEnv`
on the server. Wrapped sessions:

- support `--shell bash` only;
- check the cwd only when `--cwd` is given, against the target's path
  exactly;
- are always clean; `--profile` is refused;
- on `close`, get `exit` typed into the shell (after a Ctrl-C) before the
  local wrapper process tree is killed, because killing the `docker exec` or
  `ssh` client does not reliably end what it started on the other side.

`movie term wait SESSION [--seconds 60]` blocks until the session is ready,
replacing the polling loops the docs currently show in PowerShell and Bash.
`run`, `key`, and `watch` exit 2 at once if it is not ready.

### Filming

`run SESSION 'cmd'` refuses unless the shell is at a prompt, so keys cannot
land in a running program's stdin. It writes the command and Enter to the pty
directly, then films at 5 fps into `--record DIR` until the next prompt plus
`--hold` seconds (default 1.5), or `--seconds`. `watch` films without input,
waiting for the prompt after the last `run` or `key`. Each prints JSON with
`outcome`, `ok`, `exit_code`, `cwd`, and frame count, and writes `take.json`
(including a ready-to-paste `frames` scene) beside the frames. Record and
session directories must be new or empty, checked before any input is sent.

`key SESSION NAME` takes the names today's recorder takes: `Enter`,
`Escape`, `Tab`, `ArrowUp`, `ArrowDown`, `ArrowLeft`, `ArrowRight`,
`Ctrl-C`, or one printable character. Before writing arrow keys it reads the
cursor-key mode from the page (xterm.js tracks it), so arrows are sent as
`ESC O x` in application mode and `ESC [ x` otherwise, as a real terminal
would.

Frame timing is today's rule, as a pure function over capture timestamps:
frames land on the 5 fps grid, a slow capture repeats the previous frame into
missed slots, and the take ends exactly at its endpoint.

### Cleanup

`close` asks the daemon to exit and waits up to 30 s for its confirmation.
The daemon kills the whole process tree of everything it started, and
nothing else, using only process handles it holds, never PIDs read back from
files. On Unix it walks descendants of the shell and Chrome (a pty child
starts its own session, so a process group is not enough). On Windows it
uses `taskkill /T /F` on each child it started, as today, and it also places
itself in a kill-on-close job object at startup, so if the daemon itself is
killed its children die with it. `close` exits 1 when cleanup cannot be
confirmed.

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
  series, cue timing, scene validation, staleness rules, narration
  acceptance (including a one-word preamble and a dropped clause for
  `openai-chat`), status-prompt parsing (BEL and ST terminators, semicolons
  in cwd), key encoding in both cursor-key modes, the frame scheduler, the
  launcher's platform mapping, and Chrome candidate ordering per platform.
- **Black-box tests** run the built binary against movies generated at test
  time from ffmpeg's lavfi sources, ported from today's real-media suites
  (`test_assembly`, `test_checker`, `test_paths`, and the real-media parts of
  `test_subtitles`, including the burn-failure fallback), plus an unnarrated
  build and a build whose only speech is inside a `movie` scene. Paths with
  spaces, quotes, percent signs, brackets, and non-ASCII characters stay
  covered.
- **Card rendering**: a real browser renders a card at an awkward path, and a
  render timeout kills only the processes it started.
- **`term`** tests run a real pty and real headless Chrome: run, key (arrows
  inside a program that turns on application cursor mode), watch,
  still-running, failed commands, a program that queries the cursor position,
  and close killing the shell tree while sparing an unrelated process.
  `MOVIE_TEST_SHELL` and `MOVIE_TEST_SHELL_EXE` select the shell, as today.
- **The freshness test** under "Shipping".
- **Narration** tests that need the network or Piper run when a key or
  `piper` is present.
- **Skips are loud and can be made fatal.** Every skip names what is missing.
  `MOVIE_REQUIRE=chrome,piper,openai,...` turns the named skips into
  failures, replacing `run-tests.py --require-capabilities`.
- **CI**: a GitHub Actions matrix on macOS, Linux, and Windows. The Linux job
  installs Chrome, Piper, and the default voice and sets
  `MOVIE_REQUIRE=chrome,piper`, so narration and narrated builds are always
  exercised. The Windows job runs the `term` tests under PowerShell 5.1,
  PowerShell 7, and Git Bash.

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
   Chrome discovery for cards. Deletes the remaining pipeline scripts,
   `media_paths.py`, `narration_contract.py`, their tests, the ASR docs, and
   the Windows pipeline sections; converts `examples/e2e/scenes.yaml`; and
   updates the e2e `Dockerfile` and README to install the `piper` CLI.
   `browser_tools.py` stays, because the Windows recorder still imports it.
3. **`term`**: ConPTY spike, then the recorder. Deletes both
   `film-terminal.py` files, `browser_tools.py`, and their tests; rewrites
   `recording-a-terminal.md` and moves the e2e demo to a wrapped
   `docker exec` session.
4. **`browse`**: its own design first.

## Risks

- **ConPTY behavior on Windows** is the largest unknown; the spike decides it.
- **Terminal queries on Windows**: ConPTY answers some queries itself; the
  spike confirms replies from the page do not double up.
- **Piper's CLI** is Python-packaged, so the keyless voice needs uv to install
  it once. The skill does not do that install itself.
- **Repo size** grows by five binaries (on the order of 10 MB each) per
  release that changes build inputs.
- **The pace range** is a guess until tuned against real clips, and it is a
  coarse check by design.
