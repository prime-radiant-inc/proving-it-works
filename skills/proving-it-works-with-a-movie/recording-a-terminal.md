# Recording a terminal

CLIs, TUIs, installs, test runs, agents at work: a large share of what is
worth proving happens in a terminal. `movie term` films one. tmux holds a
clean bash; you drive it one command at a time, with your own tool calls, a
sub-agent, or a script; `stop` renders what it showed into frames for
`movie build`. It runs on macOS, Linux, and WSL (on Windows, run it inside
WSL).

```bash
m="$SKILL_DIR/bin/movie"
"$m" term start demo/ --title "todo" --subtitle "a tiny todo list, proven on camera"
"$m" term run demo/ './todo add buy milk' --say "We add an item."
"$m" term run demo/ './todo add write the report'
"$m" term run demo/ './todo done 1' --say "Add a second, and mark the first one done."
"$m" term run demo/ './todo list' --say "The list shows the first item checked off."
"$m" term stop demo/ demo/takes/            # renders the takes, writes demo/takes/scenes.yaml
"$m" build demo/takes/scenes.yaml todo.mp4
```

That is the whole route. `run` types a command at human pace, waits for
the prompt, and prints the command's exit code and output, so you see what
happened without looking at a frame.

**`--say` is how you narrate.** It ends a beat: the sentence narrates
everything since the previous `--say`, and plays from the beat's start,
over the commands as they run. So do the work, then say what it proved:
above, `add write the report` runs unnarrated and `done 1 --say "Add a
second, and mark the first one done."` narrates both. Say what the beat
proves ("the tests pass"), not what is being typed. Keep beats to a
command or two, so the words play while the work is on screen.

**`stop` writes the scene file.** `scenes.yaml` beside the takes holds the
title card (from `start --title`), then one scene per beat with its
sentence, starting with the beat. When a long beat's sentence is about
output that only appears at its end, such as a slow test run, add
`narration_at: end` to that scene: the narration then never starts before
the result is on screen, and the result stays up until it finishes. It is
an ordinary scene file (assembling.md): reword it, or add image, card, or
movie scenes, before you build.

Time the shell spends waiting at a prompt for your next command is cut to
1.5 seconds, so pausing to think between commands never puts dead air in
the movie. `screen` prints what is on screen right now, for driving a TUI.

## The verbs

| Verb | Does | Exit |
|---|---|---|
| `start SESSION [--cwd DIR] [--size 120x34] [--title T] [--subtitle S] [-- WRAPPER...]` | start the session and the recorder, then return | 0, or 2 |
| `run SESSION 'cmd' [--say "sentence"] [--timeout 60]` | type the command at human pace, press Enter, wait for the prompt; print the outcome, then the command and its output (all of it, even if it scrolled) | 0 succeeded, 1 failed, 2 refused (a command is still running), 3 still running at the timeout |
| `type SESSION 'text'` | type into whatever is running, such as a TUI's input box | 0 |
| `key SESSION NAME` | `Enter`, `Escape`, `Tab`, `Up`, `Down`, `Left`, `Right`, `C-c`, or one character | 0 |
| `wait SESSION [--quiet S] [--timeout 60]` | wait for the prompt (0 or 1), or for the screen to hold still for S seconds (3, still running) | |
| `screen SESSION` | print the screen as text | 0 |
| `cut SESSION` | end this take, holding its result, and start the next (`run --say` does this for you) | 0 |
| `film SESSION on\|off` | keep what follows out of the movie; each `on` starts a new take | 0 |
| `stop SESSION OUTDIR [--px 1600x900]` | end the session, render every take, and write `OUTDIR/scenes.yaml` | 0 |
| `render SESSION OUTDIR [--px 1600x900]` | render again from the recording, even after the session is gone | 0 |

`run` refuses while a command is still running, so keys never land in a
running program's stdin; `wait` for it, or send `key C-c`. Session and
output directories must be new or empty.

## Long work does not belong inside one take

An agent run or a build takes minutes. Film the command being issued, then
`film off`, `wait` for it, `film on`, and film the result: the movie cuts
from the command to its result, and a card in the scene file can say how
long it took. The work is real; the tedium is not. Time a command spends
running is never compressed; only time spent waiting at a prompt for the
next command is.

`film off` films the screen as it stands and holds it for 1.5 seconds
before the take ends, as `stop` does, so the result of the last `run` is
always in the take.

If tmux stops answering (the shell exited, the container stopped), the
recorder gives up after five failed polls in a row, and `stop` and `render`
print a `WARN` saying why the recording ended. The failures are in
`SESSION/recorder.log`.

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
