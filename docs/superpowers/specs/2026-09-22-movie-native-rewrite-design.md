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
| Terminal | Code: run a scripted session in a pty, record the output, render frames offline. No browser. Gated on a spike. |
| Browser apps, desktop capture, log reels, stills | Instructions in the skill, not code. |

## Commands

| Command | Does |
|---|---|
| `movie build SCENES.yaml OUT.mp4` | narrate, assemble, subtitle, burn, check |
| `movie check MOVIE` | the gate, on any movie |
| `movie term SCRIPT.yaml OUTDIR` | film a scripted terminal session into frames |

Exit codes: 0 success; 1 negative verdict (not shippable, narration
rejected, a filmed command failed or timed out); 2 usage or environment
error.

## Layout and shipping

```
cmd/movie/                  flag parsing and dispatch
internal/scene/             parse and validate the scene file
internal/ffmpeg/            run ffmpeg/ffprobe
internal/narrate/           engines, clip cache, openai-chat gate
internal/build/             segments, cards, concat, subtitles, burn
internal/check/             sampling and the verdict
internal/term/              pty session, recording, emulator, renderer
skills/proving-it-works-with-a-movie/bin/
    movie                   sh launcher (macOS, Linux, Git Bash)
    movie-darwin-arm64  movie-darwin-amd64  movie-linux-amd64
    movie-linux-arm64   movie-windows-amd64.exe
script/build-binaries
```

Decisions are pure functions over plain data (the check verdict, cue timing,
the openai-chat gate, scene and script validation, key encoding, frame
timing, idle-gap handling), tested directly with no mocks.

**Launcher.** `bin/movie` maps `uname` to a binary and `exec`s it with
`"$@"`. On Git Bash (`MINGW*`/`MSYS*`/`CYGWIN*`) it first exports
`MSYS_NO_PATHCONV=1` and `MSYS2_ARG_CONV_EXCL='*'` so Git Bash does not
rewrite Unix-looking arguments; paths from Git Bash are passed in Windows
form (`cygpath -m`). PowerShell runs the `.exe` directly. No `.cmd`
launcher, because cmd.exe mangles arguments.

**Build.** `script/build-binaries` builds all five with `GOTOOLCHAIN` set to
the exact version in `go.mod`'s `toolchain` line, `GOAMD64=v1`,
`GOARM64=v8.0`, `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`,
`-ldflags "-s -w"`. `.gitattributes` forces LF on sources and marks `bin/*`
binary. A test rebuilds all five and byte-compares them with the committed
ones. Binaries are committed with the source change that alters them.

**Prerequisites.** ffmpeg and ffprobe. For the keyless voice, Piper
(`uv tool install piper-tts`) and a voice (`uvx --from piper-tts python -m
piper.download_voices --data-dir DIR VOICE`); the tool never runs these, and
a missing one is an error naming the command. The tool needs no browser;
filming a browser app uses whatever browser automation the agent has.

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
    frames: install/         # directory of PNGs, lexical order
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
| `duration` | card, image | 3 s minimum hold |
| `narration` | card, image, frames | none |

A scene's kind is whichever of `card`, `image`, `frames`, or `movie` it
has. Paths are relative to the scene file. `engine: auto` picks `openai`
when `OPENAI_API_KEY` (or `llm keys get openai`) yields a key, else `piper`.

Validation runs before any work and reports every problem at once with its
scene id: unknown keys, zero or two kinds, a field on a kind that does not
take it, bad or duplicate ids, missing files, empty frame directories,
non-positive numbers.

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
3. **Assemble** each scene into `demo.build/<id>.mp4` at `size` and `fps`,
   lasting max(narration, visuals): short video freezes its last frame,
   short audio pads with silence. Frames are piped to ffmpeg's stdin
   (`image2pipe`), so no user path appears inside a pattern. Cards are drawn
   in Go with the embedded Go fonts (title and subtitle, centered, wrapped).
   `movie` scenes are scaled to fit and keep their own audio, with a silent
   track added if they have none. Segments are concatenated from a list of
   their fixed safe names, run from the scratch directory, so nothing needs
   escaping.
4. **Subtitle**, only if anything is narrated: cues timed inside each
   narrated scene's measured interval at its offset in the cut (today's
   millisecond allocation), written to `demo.srt`.
5. **Burn**, only if anything is narrated: copy the SRT into scratch as
   `captions.srt` and burn it with the `subtitles` filter, boxed as today.
   If ffmpeg lacks that filter, embed a soft track instead and print a
   `WARN` saying the subtitles are not in the picture. A burn that fails
   with the filter present is an error.
6. **Check** the result, expecting audio and subtitles exactly when
   something is narrated, and comparing subtitle coverage with the end of
   the last narrated scene.

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

Record, then render. One process runs a scripted session in a real pty,
records every byte the shell writes with its timestamp, and afterwards
replays that recording through a terminal emulator and draws frames. No
browser, no daemon, no session files, and no screenshot lag: the frames
show exactly what the terminal showed, when it showed it.

### Spike first

Two questions decide whether this works, and the spike answers both before
planning:

1. **Emulator fidelity.** Replay recordings of real sessions through a Go
   terminal emulator (`charmbracelet/x/vt`, else `hinshun/vt10x`) and render
   them: a plain shell, colours, a full-screen TUI (`htop` or `less`), and a
   Claude Code session. The frames must be indistinguishable in content from
   a real terminal. The emulator must also answer the cursor-position and
   device-attribute queries programs send, or those programs stall.
2. **Windows.** With go-pty on real Windows, spawn PowerShell 5.1,
   PowerShell 7, and Git Bash with the status prompt installed at launch;
   write input and read output; check whether Git Bash loses its first
   input and whether ConPTY's own query handling interferes.

It also checks a `docker exec` wrapped session on the e2e image. If
fidelity fails, the fallback is today's approach (xterm.js in headless
Chrome) and the spec is revised before planning.

### The script

```yaml
shell: bash                  # bash | powershell | pwsh; default bash
cwd: work/                   # default: the script's directory
size: 120x34                 # columns x rows; default 120x34
wrap: [docker, exec, -it, provdemo]   # optional, bash only

steps:
  - run: claude plugin list
  - pause: 2
  - run: claude plugin install proving-it-works
    timeout: 120
  - run: claude -p "make the movie"
    film: false              # happens, but off camera
    timeout: 900
  - run: ls -la out/
  - key: q
```

| Step | Does |
|---|---|
| `run: CMD` | wait for the prompt, type `CMD` at human pace, press Enter, wait for the next prompt (up to `timeout`, default 60 s) |
| `key: NAME` | press `Enter`, `Escape`, `Tab`, an arrow, `Ctrl-C`, or one character; arrows follow the terminal's cursor-key mode |
| `pause: SECONDS` | hold |
| `wait: SECONDS` | wait for output to go quiet for that long, for TUIs with no prompt to wait for |

Any step can carry `film: false`. A `run` whose command fails, or that
times out, stops the script with exit 1 unless it has `may_fail: true`.
Typing only ever happens at a prompt, so keys cannot land in a running
program's stdin.

### Output

`movie term demo-install.yaml takes/install` writes:

- `takes/install/take-1/`, `take-2/`, ...: one PNG directory per stretch of
  filmed steps (a `film: false` step ends one take and the next filmed step
  starts another), rendered at 10 fps, each with a `take.json` holding a
  ready-to-paste `frames` scene;
- `takes/install/session.log`: the raw recording, with timestamps, for
  re-rendering or inspection;
- a one-line summary per step on stdout: command, exit code, duration.

Frames are 1600×900 by default (`--px WxH`), drawn with an embedded
monospace font with wide glyph coverage (DejaVu Sans Mono, under its
permissive license). Output quiet for more than 3 s inside a filmed take is
shortened to 3 s, so a slow step does not become dead air; the step summary
reports the real durations.

### The shell

The shell always starts clean, with a status prompt installed at launch that
reports sequence number, success, exit code, and cwd in an OSC title
sequence the renderer never draws:

- **bash:** `bash --noprofile --norc -i` with `PROMPT_COMMAND` and `PS1` in
  its environment; the prompt captures `$?`, then `export -n PROMPT_COMMAND`
  so nested shells do not inherit it (verified on bash 3.2). On Windows,
  `bash` means Git Bash, found under `ProgramFiles`, `ProgramW6432`, or
  `LOCALAPPDATA\Programs`, never on PATH (which may hold WSL's);
  `--shell-exe` overrides. The launcher's `MSYS_*` variables are removed
  from its environment.
- **PowerShell:** `-NoLogo -NoProfile -NoExit -EncodedCommand <prompt
  script>`.
- **wrap:** the wrapper is a prefix; `movie term` appends
  `env PROMPT_COMMAND=... PS1=... bash --noprofile --norc -i`, so the binary
  stays on the host and nothing of ours enters the container. It works with
  wrappers that pass arguments through unchanged; ssh is not supported.

When the script ends or fails, `movie term` kills the process tree it
started (Unix: descendants; Windows: `taskkill /T /F`) using the handles it
holds.

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

## Testing

- Go `testing`, no mocks.
- Unit tests for the pure functions listed under "Layout".
- Black-box tests run the binary on movies generated at test time with
  ffmpeg's lavfi sources, ported from today's `test_checker`,
  `test_assembly`, `test_paths`, and `test_subtitles`, plus an unnarrated
  build and awkward paths (spaces, quotes, `%`, brackets, non-ASCII).
- `term` tests run real scripts in a real pty: a failing command, a timeout,
  `film: false` splitting takes, arrows in application cursor mode, a
  program that queries the cursor position, idle shortening, and cleanup
  sparing an unrelated process. Rendering is tested by replaying committed
  recordings and comparing frames with committed expected PNGs.
  `MOVIE_TEST_SHELL` and `MOVIE_TEST_SHELL_EXE` pick the shell.
- Tests needing Piper or a key skip with a message naming what is missing.
- CI on macOS, Linux, and Windows. Setup installs Piper and fails if it
  cannot, so those tests never skip in CI. Windows runs `term` under
  PowerShell 5.1, PowerShell 7, and Git Bash.

Python tests are deleted with the code they cover, after their real
behaviors are carried into Go tests; the mock-world tests are not ported.

## Order

1. **Core and `check`**: module, launcher, build script, freshness test, CI,
   `movie check`. Deletes `check-movie` and its tests.
2. **`build`**. Deletes the other pipeline scripts, `media_paths.py`,
   `narration_contract.py`, their tests, the ASR and Windows-pipeline docs;
   converts `examples/e2e/scenes.yaml`; installs the `piper` CLI in the e2e
   Dockerfile. Keeps `browser_tools.py`, which the Windows recorder imports.
3. **`term`**: spike, then the tool. Deletes both `film-terminal.py` files,
   `browser_tools.py`, and their tests; rewrites `recording-a-terminal.md`;
   moves the e2e demo to `movie term` scripts with `wrap`.
4. **Instructions**: rewrite `recording-motion.md` (browser apps and desktop
   windows), `rendering-from-a-log.md`, `rendering-stills.md`, and SKILL.md
   around the three commands.

## Known losses

- No per-stage commands; rerunning any part reruns `build` (narration is
  cached).
- No phonetic respelling separate from the subtitles.
- Cards use the Go fonts: Latin, Greek, and Cyrillic only.
- `movie` scenes fill the frame; no inset or gain control.
- No subtitle styling or cue-length options.
- Rejected narration attempts are not kept on disk.
- Terminal sessions are scripted up front, not driven step by step across
  tool calls; always clean (no user startup files); no zsh; no ssh.
- No tool for browser apps or desktop capture; agents follow the
  instructions.

## Risks

- Emulator fidelity for full-screen TUIs, and ConPTY on Windows; the spike
  decides both.
- Piper's CLI is Python-packaged, so the keyless voice needs uv once.
- Five binaries, roughly 10 MB each, per release that changes Go source.
