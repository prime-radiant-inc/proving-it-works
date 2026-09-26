# Recording a desktop app

`movie desk` films a desktop app on an X11 display the way `movie browse`
films a web page. You look at the screen with `shot`, act with real
pointer and key input, and narrate what you saw with `say`. A background recorder
films the screen, and `stop` renders the takes and writes the scene file.
It needs Linux with X11: a desktop session, or Xvfb in a container. It uses
`xdotool` and `ffmpeg`, which must be installed wherever the display is.

```bash
m="$SKILL_DIR/bin/movie"
# the app runs on display :99 in a container; start it yourself
docker exec -d -e DISPLAY=:99 app-box blender
"$m" desk start demo/ --display :99 --title "Blender" -- docker exec app-box
"$m" desk shot demo/                       # look: prints demo/shot.png and the pointer
"$m" desk click demo/ 660 420              # select the cube
"$m" desk key demo/ x
"$m" desk click demo/ 633 420              # Delete, in the confirmation
"$m" desk shot demo/                       # look at the result, and read it
"$m" desk say demo/ "I select the cube and delete it."
"$m" desk stop demo/ demo/takes/
"$m" build demo/takes/scenes.yaml blender.mp4
```

**Use the app the way a person would.** Look, do one thing, look again:
take a `shot`, read it, act, `shot` again to see what happened. Menus open
late, and buttons ignore clicks until the pointer has settled, so never
fire a sequence of clicks blind. What worked can become a script later, if
the movie needs to be rerun.

**Look before you narrate.** `say` is refused until a `shot` has been taken
since the last action, and a shot only helps if you read it: say what it
shows, not what you meant to happen. A misread button position turns
12 × 34 into 11 × 24 without a single error. An app can hold still and then change: a
software-rendered preview, a dialog, or a file load can land seconds after
the click, after the verb has already settled. A `wait --quiet 3` gives slow
apps room. A result that shows up late also means the click worked; a
second click on a toggle can undo it.

**Coordinates are pixels of what is filmed**: read a point off the shot
and click it. Without `--window`, the whole display is filmed. With
`--window NAME`, only the area of the first visible window whose title
contains NAME (a regular expression) is filmed, and coordinates are
relative to it; `start` prints which window it found. That area is fixed
when filming starts: if the window moves or resizes, or a menu or dialog
opens outside it, the movie will not show it and you cannot click it.
Film the whole display when the app opens windows of its own.

**Keys go to the window under the pointer** on a display with no window
manager, as Xvfb has. Move the pointer onto the app before `key` or `type`.

**Anything that keeps moving keeps the picture from holding still**: a
clock, a spinner, a blinking caret. Then the time you spend thinking is not
cut, and `wait` times out. Film just the app with `--window` to leave a
panel clock out.

**`say` is how you narrate.** It ends a beat: the sentence narrates
everything filmed since the last `say`, and the next action starts a new
take. Say it once the result proving the point is on screen and you have
seen it in a shot. For slow work, such as a render: `wait --quiet 2
--timeout 120`, `shot`, then `say`.

**Keep beats short.** The sentence plays from the start of its beat, over
the clicks it describes. So `say` after a step or two (`2 + 2 =`), not
after a whole session: a long beat is silent once its sentence ends, and
would speak the result before it is on screen. When a long beat can't be
split and its sentence is about the result at its end, add
`narration_at: end` to that scene in `scenes.yaml`, so the words land on
the result.

Time spent waiting for your next action while the screen holds still is cut
to 1.5 seconds, so looking and thinking leave no dead air. Time the app
spends working is kept.

## The verbs

| Verb | Does | Exit |
|---|---|---|
| `start SESSION [--display :99] [--window NAME] [--title T] [--subtitle S] [-- WRAPPER...]` | start filming the display or one window | 0, or 2 |
| `shot SESSION [PNG]` | save what is filmed now; print its size and the pointer position | 0 |
| `move SESSION X Y` | glide the pointer to X,Y | 0 |
| `click SESSION X Y [--right] [--double]` | glide there, pause, and click | 0 |
| `drag SESSION X1 Y1 X2 Y2` | press, glide, release | 0 |
| `key SESSION KEY...` | press keys by xdotool's names: `Return`, `Escape`, `ctrl+s`, `shift+a`, `KP_1` | 0 |
| `type SESSION 'text'` | type at human pace into whatever has the focus (`type SESSION -- '-5'` for text that starts with a dash) | 0 |
| `wait SESSION [--quiet 1] [--timeout 60]` | wait until the picture holds still | 0, 1 timed out |
| `say SESSION "sentence"` | end a beat, narrated by the sentence; refused until a shot since the last action | 0, 2 |
| `cut SESSION`, `film SESSION on\|off` | as in `browse` | 0 |
| `stop SESSION OUTDIR`, `render SESSION OUTDIR` | render the takes and write `OUTDIR/scenes.yaml` | 0 |

After each action the verb waits for the picture
to hold still for 0.4 seconds, for at most 5 seconds. Any verb exits 2 when
xdotool or ffmpeg fails, and every action refuses to run once the recorder
has stopped filming, with ffmpeg's reason: stop that session and start a
new one.

## In a container

```bash
docker run -d --init --name app-box IMAGE sleep infinity
docker exec -d app-box Xvfb :99 -screen 0 1600x900x24 +extension GLX
"$m" desk start demo/ --display :99 -- docker exec app-box
```

With `-- docker exec CONTAINER`, xdotool and ffmpeg run in the container
and the recording stays on the host. `--init` matters: without it, exited
processes stay behind as zombies. Apps drawing with OpenGL, such as
Blender, get it from Mesa (`libgl1-mesa-dri`); `+extension GLX` gives Xvfb
the GLX extension they need.

A first run may put up a splash screen or a first-run dialog. Dismiss it
before you narrate, or with `film off` around it.

## macOS

`movie desk` does not drive macOS yet: moving the real pointer there needs
native code. To film a Mac app, capture it with ffmpeg as recording-motion.md
describes. Terminal (or whatever runs the agent) needs Screen Recording
permission, and Accessibility to send keystrokes with AppleScript.
