# `term` spike findings

Date: 2026-09-23
Status: findings for Tasks 16-18 of the native rewrite plan
Spec: `2026-09-22-movie-native-rewrite-design.md`, section `term`

## Verdict

No assumption in the spec's `term` section failed badly enough to change the
design. The spike raised four points for Tasks 16-18, all additions rather
than redesigns. The controller has ruled on each:

1. **Locale (Task 16).** With the default (C) locale, multibyte text typed
   with `send-keys -H` is corrupted on both macOS and Linux, and readline can
   even insert text from history. Ruling: the session command sets
   `LANG=C.UTF-8`.
2. **Renderer (Task 17).** Task 17's `ParseLine` as written draws OSC 8
   hyperlinks as visible text, gives combining marks their own cell, and
   ignores SGR 2 (dim). Ruling: Task 17 skips OSC sequences, keeps zero-width
   runes out of their own cells, and draws dim.
3. **Fallback fonts (Task 17).** Ruling: the chain is DejaVu Sans Mono, then
   Noto Sans Symbols 2, then Noto Sans Symbols, which together add 817 KB.
   JuliaMono alone would also cover everything, but it costs 3.2 MB, about
   12 MB more across the five release binaries.
4. **Leaked agent variables (Task 16).** The shell inherits the invoking
   agent's `CLAUDECODE` and `CLAUDE_CODE_*` variables, including
   `CLAUDE_CODE_MESSAGING_TOKEN`. A nested Claude Code showed a warning about
   them on camera. The spec's list of scrubbed variables doesn't cover them.
   Ruling: Task 16 strips `CLAUDECODE` and every `CLAUDE_CODE_*` variable
   from the filmed shell.

## What was recorded

The skeleton binary (`go build -o /tmp/movie-spike ./cmd/movie`) recorded
every session, with keys sent through `tmux -S <socket> send-keys`.

| Host | tmux | bash | Sessions |
|---|---|---|---|
| macOS 26.5.2 (Darwin 25.5, arm64) | 3.5a | 3.2.57 | shell (`ls --color`, `git log --oneline --graph --color`, a styled `printf`); `less -R` on the spec; `top -o cpu -s 1`, then `vim -u NONE`; Claude Code 2.1.281 (two one-line prompts, one of which ran `echo hi` to show `⎿`, shift+tab through the modes, `/exit`); an SGR probe (below) |
| Linux container: the e2e image (`examples/e2e/Dockerfile`, node:22-bookworm-slim), aarch64 under OrbStack, driven with `-- docker exec CONTAINER` | 3.3a | 5.2.15 | shell (`ls --color -la`, `git log --graph --color --all` in a repo with a merge); `less` on a 500-line file; `htop` 3.2.2 for a few seconds; Claude Code (installed by the image but not logged in, so only the onboarding screen) |

htop isn't installed on this Mac, so macOS got `top` and `vim` in its place.
The e2e image lacks `less` and `htop`; the spike installed them in its
throwaway container. The spike removed its containers when it finished.

## Decision: escape sequences the renderer must handle

An inventory of all recordings (675 snapshots) and a probe that printed every
SGR attribute, an OSC 8 hyperlink, a tab, a wide character, an emoji, braille,
and a combining accent found:

- **CSI:** only SGR (`ESC [ ... m`). No cursor movement, erase, or mode
  sequences, on either tmux version.
- **SGR parameters seen in the real sessions:** `0`, `1`, `2`, `4`, `7`,
  `30`-`37`, `39`, `40`-`47`, `49`, `90`-`97`, `38;5;N`, `48;5;N`,
  `38;2;R;G;B`, `48;2;R;G;B`. Claude Code uses `2` (dim) heavily for secondary
  text (248 runs on macOS, 95 in the container). Task 17 should draw dim, for
  example as the foreground blended halfway to the background. Right now its
  `applySGR` ignores dim.
- **More SGR that tmux 3.5a can emit (seen only in the probe):** `3`, `5`, `8`,
  `9`, `100`-`107`, `58;5;N` (underline colour), and colon forms `4:2`/`4:3`
  (double/curly underline) and `5:3` (what tmux writes for overline). tmux
  rewrites `38:2::R:G:B` as `38;2;R;G;B`, and it turns attribute-off codes
  (`22`, `24`, ...) into a full `0` reset followed by what stays on. Task 17's
  `applySGR` splits on both `;` and `:`. That handles all of these correctly,
  because `4:2` and `4:3` both still set underline and the trailing number
  lands on an ignored code. The one input it would get wrong is `4:0`, which
  tmux never emits.
- **Non-CSI escapes:** OSC 8 hyperlinks, `ESC ] 8 ; ; URL ESC \`, appear in
  tmux 3.5a's `capture-pane -e` output whenever the program wrote one. tmux
  3.3a doesn't support hyperlinks and never emits them. None of the recorded
  real sessions contained one, but `ls --hyperlink`, gcc, and Claude Code can
  all write them. **Task 17 must skip `ESC ]` through its terminator (`ESC \`
  or BEL).** As written, `ParseLine` treats only `ESC [` specially, so it
  would draw the ESC, the `]8;;` and the URL.
- **Control characters:** none. tmux expands tabs to spaces.
- **Line shape:** `capture-pane -p` prints exactly `rows` lines, each ending in
  a newline, with trailing blanks trimmed. Short lines must be padded, as
  `ParseLine` does.
- **Wide characters:** tmux emits a wide character with no padding cell after
  it, so the parser must place the second half itself, as `ParseLine` does.
  tmux's widths and `golang.org/x/text/width` agree on every character the
  sessions printed. The one disagreement found is `☰` U+2630, which tmux 3.5a
  on macOS draws 2 cells wide (x/text says 1, tmux 3.3a says 1). Nothing
  recorded printed it.
- **Combining marks:** `e` + U+0301 comes out as two code points that tmux
  keeps in one cell. `ParseLine` would give the accent a cell of its own and
  shift the rest of the line right. **Task 17: zero-width runes (Unicode Mn and
  Me, U+200D, U+FE0F) must not take a cell.** Either NFC-compose the line first
  (`golang.org/x/text/unicode/norm`; `golang.org/x/text` is already in go.mod) or drop
  them. Only the probe produced one.

## Decision: fallback fonts

These are every non-ASCII character the recordings contain that DejaVu Sans
Mono can't draw, according to `fonts.Missing`:

| Character | Seen in |
|---|---|
| `⏺` U+23FA | Claude Code (143 times on macOS) |
| `⏵` U+23F5 | Claude Code mode line (`⏵⏵ auto mode on`) |
| `⏸` U+23F8 | Claude Code mode line (`⏸ plan mode on`) |
| `⎿` U+23BF | Claude Code tool output (`⎿  hi`) |
| `漢` U+6F22 | probe only; CJK is out of scope and gets the replacement box |

DejaVu Sans Mono already covers everything else the sessions printed: Claude
Code's spinner (`✢ ✳ ✶ ✻ ✽ ·`), `❯ ⚠ ◐ ✔ ✓ … — ↓ →`, block elements
(`▐▛█▜▝▀▄░▓`), box drawing (`─│┌┐╌`), and `▶ ▽`. No session printed braille,
but DejaVu Sans Mono has none of its 256 code points, so the chain must supply
them. Coverage was also checked against all of U+2800-28FF, U+2500-259F,
U+23E9-23FA, and a handful of other CLI symbols.

Candidates (coverage from a scratch `sfnt` program):

| Font | ⏺ ⏵ ⏸ | ⎿ | Braille | Size |
|---|---|---|---|---|
| DejaVu Sans Mono (the terminal font) | no | no | no (0 of 256) | - |
| Noto Sans Math | no | no | no | 642 KB (657,440 bytes, static unhinted; the google/fonts build is 1.0 MB) |
| Noto Sans Symbols 2 | yes | **no** | yes (256 of 256) | 656 KB |
| Noto Sans Symbols | no | yes | no | 142 KB |
| JuliaMono Regular | yes | yes | yes | 3.2 MB |

DejaVu Sans, the proportional card font, does have braille, but it isn't part
of the terminal chain. Every Noto font tried lacks `⎿` except Noto Sans
Symbols. The Noto fonts also lack `⏺`, except Noto Sans Symbols 2.

**Chosen (controller ruling):** Noto Sans Symbols 2 and Noto Sans Symbols,
817 KB together. JuliaMono would also cover everything in one font, but it
costs 3.2 MB per binary, about 12 MB more across the five release binaries.

**Chain order for Task 17:** DejaVu Sans Mono, then Noto Sans Symbols 2, then
Noto Sans Symbols: `fallbacks = [NotoSansSymbols2, NotoSansSymbols]`. For every
character in this spike the two Noto fonts are disjoint, so the order between
them changes nothing today. Symbols 2 goes first because it covers the most.

**Verified.** A scratch coverage check ran that chain over every recording
from both hosts plus the probe ranges: 476 distinct non-ASCII characters.

- Every character a real session printed is covered. `⏵ ⏸ ⏺` and all 256
  braille code points come from Noto Sans Symbols 2, and `⎿` from Noto Sans
  Symbols.
- Still missing from the chain, none printed by a real session:
  - `漢 字 😀`: CJK and emoji, out of scope, typed by the probe.
  - `ℹ` U+2139, `⏫` U+23EB, `⏬` U+23EC, `⏰` U+23F0: these appear only in the
    spike's synthetic probe list. tmux draws the last three 2 cells wide, as
    emoji. They get the replacement box and a `WARN`, as the spec intends.

Noto's `⏺` is wider than a cell: its advance is 25 px against a 17 px DejaVu
Sans Mono cell at 28 px. Task 17 should draw fallback glyphs centred in the
cell. Clipping or scaling them isn't needed at the sizes seen.

Files in `internal/fonts/` (not embedded yet; Task 17 wires them in):

| File | Source | SHA-256 | Size |
|---|---|---|---|
| `NotoSansSymbols2-Regular.ttf` | https://github.com/notofonts/notofonts.github.io/raw/main/fonts/NotoSansSymbols2/unhinted/ttf/NotoSansSymbols2-Regular.ttf (last changed in commit `c16b117609ab`, 2023-10-13) | `c4a0a80f0041ce4be81e2478faad22776d23edb98ae3f0d19bd37044820ecf9d` | 671,568 bytes |
| `NotoSansSymbols-Regular.ttf` | https://github.com/notofonts/notofonts.github.io/raw/main/fonts/NotoSansSymbols/unhinted/ttf/NotoSansSymbols-Regular.ttf (last changed in commit `1b2fe62733b8`, 2023-06-28) | `6eea9cb4cd39269ea9f95ba5c2735f80ae74049dfc9e1a7c932a5cfc8f0c3030` | 145,508 bytes |
| `LICENSE-NotoSansSymbols` | https://github.com/notofonts/symbols/raw/main/OFL.txt (the one license for both fonts, which come from the same `notofonts/symbols` project; byte-identical to google/fonts' `ofl/notosanssymbols2/OFL.txt`) | `b118dd41337806a5d4797052c77caf3bd096aed783e5eb21b4d11154351e1ac0` | 4,383 bytes |

**License:** SIL Open Font License 1.1, "Copyright 2022 The Noto Project
Authors (https://github.com/notofonts/symbols)". It names no Reserved Font
Name, and embedding in and redistributing with software is permitted.

These are the static unhinted builds. The google/fonts copies of Noto Sans
Symbols are variable fonts (`[wght]`), so they weren't used. The hinted and
unhinted Symbols 2 files are byte-identical.

## Decision: locale for the session command

`send-keys -H` sends the right bytes. The shell's locale decides whether they
survive:

| Shell locale | macOS (bash 3.2) | Linux container (bash 5.2) |
|---|---|---|
| unset (C) | **corrupted.** `echo 'é→✓⏺漢'` lost its opening quote and left bash at a `>` continuation prompt; `echo é` became `echo` | **corrupted.** Same, and readline read the high-bit bytes as Meta keys, so `M-.` inserted `clear` from history |
| `LANG=C.UTF-8` | intact (od shows `c3 a9 e2 86 92 ...`) | intact |
| `LANG=en_US.UTF-8` | intact | not available (`locale -a`: C, C.utf8, POSIX) |

**Decision for Task 16 (controller ruling):** `shellCommand` sets
`LANG=C.UTF-8` in every session, local and wrapped. It should also unset
`LC_ALL` and `LC_CTYPE` (`-u LC_ALL -u LC_CTYPE`), because either one would
override `LANG`. `C.UTF-8` was verified to work on this macOS 26.5 and in the
bookworm container. `docker exec` doesn't pass the host's environment, and
`en_US.UTF-8` isn't installed in the e2e image, so no host locale is passed
through. The spike didn't check which older macOS releases have `C.UTF-8`.

## Decision: capture rates

Each figure times the recorder's exact chained call
(`display-message -p FORMAT ; capture-pane -p -e`) back to back. The session's
own recorder was polling at the same time.

| Where | Screen | Calls/s | p50 | p95 | max |
|---|---|---|---|---|---|
| macOS, local | shell after `git log` (3.0 KB/call) | 24-29 | 31-36 ms | 65-83 ms | 189 ms |
| macOS, local | Claude Code idle (2.1 KB) | 30-121 | 8-31 ms | 11-57 ms | 120 ms |
| macOS, local | `top` running (3.3 KB) | 79-134 | 7-10 ms | 9-28 ms | 62 ms |
| docker exec (OrbStack) | shell after `git log` (2.6 KB) | 9-23 (typically 20-22) | 43-76 ms | 46-239 ms | 306 ms |
| docker exec (OrbStack) | `htop` running (3.3-4.4 KB) | 11-22 | 45-66 ms | 50-221 ms | 287 ms |
| docker exec (OrbStack) | Claude Code onboarding running | 4.9-5.1 | 196-209 ms | 269-323 ms | 404 ms |
| inside the container (no docker exec) | shell, status format reduced to one field | about 500 | 2 ms per call | | |

The local range reflects machine load; three runs of each were taken while
other work ran on the Mac. The takeaways:

- **Local:** the recorder always reaches its 10/s cap.
- **docker exec:** about 45 ms per call is `docker exec` overhead, since tmux
  itself answers in 2 ms. The recorder usually reaches 10/s, but falls to about
  5/s while a busy TUI runs in the container. No design change is needed: the
  recorder already sleeps `100ms - elapsed`, so it slows gracefully, and each
  snapshot holds until the next. No call failed. (Task 6 left a deferred note
  that any status error ends the recording; the spike saw no such error in
  more than 1,000 wrapped calls.)
- **For Task 18 (inferred, not measured):** `typeText` makes one tmux call per
  character and then sleeps the full pace. Through `docker exec`, typing would
  take about 45 ms plus 55 ms per character, nearly twice the intended pace.
  Subtracting the call's elapsed time from the sleep would fix it.

## Other assumptions checked

- **`pane_height` equals the requested rows after `status off`:** holds. 100x30
  gave 30 and 120x34 gave 34 in every snapshot on both hosts, through
  `docker exec` too.
- **Prompt marker and readiness rule under polling:** held throughout. `run`
  refused correctly while `claude` was running (`the shell is not at a prompt
  (claude is running)`) and worked again after it exited. Two observations:
  - `#{pane_current_command}` for Claude Code is `2.1.281` on macOS (the native
    binary names its process after its version) and `claude` in the container.
    The rule only compares against `bash`, so neither matters.
  - While Claude Code ran in the container, tmux 3.3a set `#{pane_title}` to
    `Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA`: Claude's kitty-graphics query, taken
    as a title. `parseTitle` rejects it, which reads correctly as not at a
    prompt, and the next prompt restores the marker. No change needed.
- **`pane_current_command` through `docker exec`:** reports `bash`, `less`,
  `htop`, and `claude` correctly.
- **`tmux -u`:** neither needed nor harmful. A server started without `-u`,
  from a caller with no locale, stored and captured UTF-8 intact on tmux 3.5a
  (macOS) and 3.3a (Linux). With no client attached, `-u` changes nothing.
  Only bash's locale matters. Keep `-u` as it is.

## Found along the way (not in the spec)

- **Agent variables leak into the take.** Local sessions inherit `CLAUDECODE`,
  `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_SESSION_ID`,
  `CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_CODE_MESSAGING_SOCKET`,
  `CLAUDE_CODE_MESSAGING_TOKEN`, `CLAUDE_CODE_EXECPATH`, `CLAUDE_PID`, and
  `CLAUDE_EFFORT`. Because of them the filmed Claude Code printed
  `⚠ Transcript saving is off — inherited CLAUDE_CODE_CHILD_SESSION marker`.
  The token would also show up if the take ran `env`. Ruling: Task 16 strips
  `CLAUDECODE` and every `CLAUDE_CODE_*` variable. The names vary by version,
  so the list is best built from `os.Environ()`. `CLAUDE_PID` and
  `CLAUDE_EFFORT` fall outside that pattern and would still pass through;
  neither triggered a visible effect. Wrapped sessions don't inherit any of
  them.
- **`COLORTERM` passes through.** On macOS the host's `COLORTERM=truecolor`
  reached Claude Code, which drew in 24-bit colour (`38;2`). The container has
  no `COLORTERM`, so the same program drew in 256 colours (`38;5`). Setting
  `COLORTERM=truecolor` in the session command would make takes look the same
  everywhere. tmux keeps RGB colours in `capture-pane -e`.
- **User pager settings pass through.** The host's `LESS=-iRFXMx4` meant
  `less` skipped the alternate screen on macOS. That's consistent with the
  spec, which scrubs only named variables, but worth knowing when a take looks
  different from a clean machine.
- **Stale sockets.** `/tmp` held 103 `movie-*.sock` files with no live server,
  timestamped from earlier task test runs today. Something in the `term`
  tests, or `kill-server` in wrapped or failed paths, leaves socket files
  behind. Task 16's tests should check.
- The skeleton's `render` printed no `WARN` for `⏺` or `漢`. That's expected
  until Task 17.

## Reproducing

The scratch programs weren't committed. They were:
- an inventory that reads `recording.jsonl` files and lists CSI finals, SGR
  attributes, non-CSI escapes, control characters, and every non-ASCII
  character with `fonts.Missing` and candidate-font coverage;
- a glyph sheet that draws Claude Code's symbols in a DejaVu Sans Mono grid
  with each candidate fallback;
- a rate timer that runs the recorder's chained call N times, optionally
  through a wrapper;
- a width check that compares tmux's cursor advance with
  `golang.org/x/text/width`.

Each is about 100 lines, and Task 17's tests cover the parts that matter.
