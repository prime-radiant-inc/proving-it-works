# Recording a terminal

CLIs, TUIs, installs, test runs, agents at work: a large share of what is
worth proving happens in a terminal. `movie term` films one. tmux holds a
clean bash; you drive it one command at a time, with your own tool calls, a
sub-agent, or a script; `stop` renders what it showed into frames for
`movie build`. It runs on macOS, Linux, and WSL (on Windows, run it inside
WSL).

```bash
m="$SKILL_DIR/bin/movie"
"$m" term start take/                       # a clean bash, 120x34, filming
"$m" term run take/ 'npm test'              # types it, waits for the prompt
"$m" term screen take/                      # what is on screen, as text
"$m" term stop take/ frames/                # frames/take-1/ + take.json
```

`frames/take-1/take.json` holds a ready-to-paste scene:
`frames: .../take-1`, `rate: 10`.

## The verbs

| Verb | Does | Exit |
|---|---|---|
| `start SESSION [--cwd DIR] [--size 120x34] [-- WRAPPER...]` | start the session and the recorder, then return | 0, or 2 |
| `run SESSION 'cmd' [--timeout 60]` | type the command at human pace, press Enter, wait for the prompt; print the outcome and the screen | 0 succeeded, 1 failed, 2 refused (a command is still running), 3 still running at the timeout |
| `type SESSION 'text'` | type into whatever is running, such as a TUI's input box | 0 |
| `key SESSION NAME` | `Enter`, `Escape`, `Tab`, `Up`, `Down`, `Left`, `Right`, `C-c`, or one character | 0 |
| `wait SESSION [--quiet S] [--timeout 60]` | wait for the prompt (0 or 1), or for the screen to hold still for S seconds (3, still running) | |
| `screen SESSION` | print the screen as text | 0 |
| `film SESSION on\|off` | keep what follows out of the movie; each `on` starts a new take | 0 |
| `stop SESSION OUTDIR` | end the session and render every take | 0 |
| `render SESSION OUTDIR [--px 1600x900]` | render again from the recording, even after the session is gone | 0 |

`run` refuses while a command is still running, so keys never land in a
running program's stdin; `wait` for it, or send `key C-c`. Session and
output directories must be new or empty.

## Long work does not belong inside one take

An agent run or a build takes minutes. Film the command being issued, then
`film off`, `wait` for it, `film on`, and film the result: the movie cuts
from the command to its result, and a card in the scene file can say how
long it took. The work is real; the tedium is not. Time inside a take is
never compressed.

## The session is clean

bash starts with no startup files and its own history file, so the take
shows none of your aliases, prompts, or past commands, and nothing typed on
camera lands in your history. Tools on a PATH set in your `.bashrc` need
their full path, or `export PATH=...` as the first `run`.

## Inside a container

```bash
"$m" term start take/ --cwd /work -- docker exec CONTAINER
```

Every tmux command then runs through `docker exec`: the shell lives in the
container, `movie` and the recording stay on the host. The container needs
tmux. `ssh` is not supported.

## Glyphs

Frames are drawn with DejaVu Sans Mono plus fallback fonts that cover the
symbols CLIs print (Claude Code's `⏺` and `⎿`, spinners, box drawing).
Anything no font covers (CJK, emoji) is drawn as a visible box, and `stop`
prints a `WARN` naming each such character. Look at the frames before you
use them.

## Playing a movie inside the terminal

`mpv --vo=tct movie.mp4` renders video as coloured terminal cells. It proves
a file plays where it was made, and it looks blocky. For a demo where the
viewer should see the movie, use a `movie` scene in the scene file instead.
