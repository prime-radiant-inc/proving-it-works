# `movie`: a native rewrite of the proving-it-works tools

Date: 2026-09-23
Status: design, simplified after two adversarial reviews, awaiting review

## Goal

A simple set of tools any agent can use to make a movie proving that
something works. Every feature below earns its place against that goal;
anything that only adds configuration or polish is out.

## Why rewrite

The pipeline today is five Python scripts the agent must run in order with
matching flags, handing state through three files each tool reads its own
way. There is no shared core, the scene file is never validated, the browser
route has no tool, the terminal route is a prose recipe on Unix and a
632-line daemon on Windows, and a third of the docs are PowerShell and Git
Bash copies of the pipeline because `uv` shebangs do not run on Windows.

## Decisions

| Decision | Choice |
|---|---|
| Language | Go, one binary. |
| Distribution | Prebuilt binaries committed to the repo. The skill never downloads executables at run time. |
| Speech recognition | None. |
| Engines | `openai`, `openai-chat`, `piper`. |
| Scene file | New format, no backward compatibility. |
| Terminal | The binary owns the pty and serves its own xterm.js page, gated on a Windows ConPTY spike. |
| `browse` | Built last, from its own design. |

## Commands

| Command | Does |
|---|---|
| `movie build SCENES.yaml OUT.mp4` | narrate, assemble, subtitle, burn, check |
| `movie check MOVIE` | the gate, on any movie |
| `movie term serve / run / key / watch / close` | film a real shell |
| `movie browse ...` | film a web app (later) |

Exit codes for every command: 0 success; 1 negative verdict (not
shippable, narration rejected, filmed command failed); 2 usage or environment
error; 3 `term` only, the filmed command is still running.

## Layout and shipping

```
cmd/movie/                  flag parsing and dispatch
internal/scene/             parse and validate the scene file
internal/ffmpeg/            run ffmpeg/ffprobe
internal/narrate/           engines, clip cache, openai-chat gate
internal/build/             segments, cards, concat, subtitles, burn
internal/check/             sampling and the verdict
internal/term/              pty session, page, keys, frames
internal/chrome/            find and drive Chrome (term, browse)
skills/proving-it-works-with-a-movie/bin/
    movie                   sh launcher (macOS, Linux, Git Bash)
    movie-darwin-arm64  movie-darwin-amd64  movie-linux-amd64
    movie-linux-arm64   movie-windows-amd64.exe
script/build-binaries
```

Decisions are pure functions over plain data (the check verdict, cue timing,
the openai-chat gate, scene validation, key encoding, the frame scheduler),
tested directly with no mocks.

**Launcher.** `bin/movie` maps `uname` to a binary and `exec`s it with
`"$@"`. On Git Bash (`MINGW*`/`MSYS*`/`CYGWIN*`) it first exports
`MSYS_NO_PATHCONV=1` and `MSYS2_ARG_CONV_EXCL='*'`, so Git Bash does not
rewrite Unix-looking arguments; paths from Git Bash are passed in Windows
form (`cygpath -m`). PowerShell runs the `.exe` directly. No `.cmd`
launcher, because cmd.exe mangles arguments.

**Build.** `script/build-binaries` builds all five with `GOTOOLCHAIN` set to
the exact version in `go.mod`'s `toolchain` line, `GOAMD64=v1`,
`GOARM64=v8.0`, `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`,
`-ldflags "-s -w"`. `.gitattributes` forces LF on sources and marks `bin/*`
binary. A test rebuilds all five and byte-compares them with the committed
ones. Binaries are committed with the source change that alters them.

**Prerequisites.** ffmpeg and ffprobe. Chrome, Chromium, or Edge for `term`
and `browse` only. For the keyless voice, Piper (`uv tool install
piper-tts`) and a voice (`uvx --from piper-tts python -m
piper.download_voices --data-dir DIR VOICE`); the tool never runs these, and
a missing one is an error naming the command.

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
    rate: 2.6
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
   in Go with the embedded Go fonts (title and subtitle, centered, wrapped)
   to a PNG, so `build` needs no browser. `movie` scenes are scaled to fit
   and keep their own audio, with a silent track added if they have none.
   Segments are concatenated with a list of their fixed safe names, run from
   the scratch directory, so nothing needs escaping.
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

**Spike first**, on real Windows with go-pty: spawn PowerShell 5.1,
PowerShell 7, and Git Bash with the prompt installed at launch; write input,
read output, resize; check whether Git Bash loses its first input; check
that the page's query replies do not double up with ConPTY's own. On the e2e
image, check a `docker exec` wrapped session. If the pty route fails, this
section becomes a port of today's ttyd route before planning.

`movie term serve SESSION --shell bash|powershell|pwsh [--shell-exe PATH]
[--cwd DIR] [--browser PATH] [-- WRAPPER...]` runs in the foreground as a
background task. It:

1. serves a page on 127.0.0.1 with vendored xterm.js at 1600×900, 17 px,
   loads it in headless Chrome (software GL), and reads the columns and rows
   xterm.js lays out;
2. spawns the shell in a pty of that size, always clean, with a status
   prompt that reports sequence number, success, exit code, and cwd in an
   OSC title sequence:
   - bash: `bash --noprofile --norc -i` with `PROMPT_COMMAND` and `PS1` in
     its environment; the prompt captures `$?`, then `export -n
     PROMPT_COMMAND` so nested shells do not inherit it (verified on bash
     3.2);
   - PowerShell: `-NoLogo -NoProfile -NoExit -EncodedCommand <prompt
     script>`;
   - on Windows, `bash` means Git Bash, found under `ProgramFiles`,
     `ProgramW6432`, or `LOCALAPPDATA\Programs`, never on PATH (which may
     hold WSL's); `--shell-exe` overrides;
   - the launcher's `MSYS_*` variables are removed from the shell's
     environment;
3. relays the pty both ways over a websocket, so xterm.js can answer the
   terminal queries programs send; the page, websocket, and control endpoint
   require a random token and check the Origin;
4. runs today's preflight (refuse a blank canvas, save `ready.png`);
5. writes `SESSION/session.json` (control address and token) and prints
   `{"ready": true}`.

With `-- WRAPPER...` (bash only), the wrapper is a prefix such as
`docker exec -it CONTAINER`, and `serve` appends
`env PROMPT_COMMAND=... PS1=... bash --noprofile --norc -i`, so Chrome and
the binary stay on the host. It works with wrappers that pass arguments
through unchanged; ssh is not supported.

`run SESSION 'cmd' [--record DIR] [--seconds N] [--hold S]` refuses unless
the shell is at a prompt, writes the command and Enter to the pty, and films
at 5 fps until the next prompt plus the hold (1.5 s), or `--seconds`.
`key SESSION NAME` takes `Enter`, `Escape`, `Tab`, the four arrows,
`Ctrl-C`, or one character, and encodes arrows by the cursor-key mode read
from xterm.js. `watch` films without input. Each waits up to 30 s for the
session to become ready, prints JSON (`outcome`, `ok`, `exit_code`, `cwd`,
frame count), and writes `take.json` with a ready-to-paste `frames` scene.
Record directories must be new or empty. A slow screenshot repeats the
previous frame so a take plays at exactly 5 fps.

`close` asks `serve` to exit; it kills the process trees it started (Unix:
descendants; Windows: `taskkill /T /F`), using handles it holds, never PIDs
from files, and exits 1 if it cannot confirm cleanup. Processes a wrapper
started on the other side (inside a container) are not its to kill.

## `browse`

Built last, from its own design. Outline: scene actions (`goto`, `wait_for`,
`click`, `type`, `pause`), a built-in cursor overlay, human-paced typing,
screenshots raced against a short timeout, output a `frames` directory.

## Testing

- Go `testing`, no mocks.
- Unit tests for the pure functions listed under "Layout".
- Black-box tests run the binary on movies generated at test time with
  ffmpeg's lavfi sources, ported from today's `test_checker`,
  `test_assembly`, `test_paths`, and `test_subtitles`, plus an unnarrated
  build and awkward paths (spaces, quotes, `%`, brackets, non-ASCII).
- `term` tests use a real pty and real headless Chrome, including arrows in
  application cursor mode, a program that queries the cursor position, a
  still-running command, and close sparing an unrelated process.
  `MOVIE_TEST_SHELL` and `MOVIE_TEST_SHELL_EXE` pick the shell.
- Tests needing Chrome, Piper, or a key skip with a message naming what is
  missing.
- CI on macOS, Linux, and Windows. Setup steps install Chrome and Piper and
  fail if they cannot, so those tests never skip in CI. Windows runs `term`
  under PowerShell 5.1, PowerShell 7, and Git Bash.

Python tests are deleted with the code they cover, after their real
behaviors are carried into Go tests; the mock-world tests are not ported.

## Order

1. **Core and `check`**: module, launcher, build script, freshness test, CI,
   `movie check`. Deletes `check-movie` and its tests.
2. **`build`**. Deletes the other pipeline scripts, `media_paths.py`,
   `narration_contract.py`, their tests, the ASR and Windows-pipeline docs;
   converts `examples/e2e/scenes.yaml`; installs the `piper` CLI in the e2e
   Dockerfile. Keeps `browser_tools.py`, which the Windows recorder imports.
3. **`term`**: spike, then recorder. Deletes both `film-terminal.py` files,
   `browser_tools.py`, and their tests; rewrites `recording-a-terminal.md`;
   moves the e2e demo to a `docker exec` wrapped session.
4. **`browse`**: its own design first.

## Known losses

- No per-stage commands; rerunning any part reruns `build` (narration is
  cached).
- No phonetic respelling separate from the subtitles.
- Cards use the Go fonts: Latin, Greek, and Cyrillic only.
- `movie` scenes fill the frame; no inset or gain control.
- No subtitle styling or cue-length options.
- Filmed shells are always clean (no user startup files); no zsh; no ssh.
- Rejected narration attempts are not kept on disk.

## Risks

- ConPTY behavior on Windows; the spike decides.
- Piper's CLI is Python-packaged, so the keyless voice needs uv once.
- Five binaries, roughly 10 MB each, per release that changes Go source.
