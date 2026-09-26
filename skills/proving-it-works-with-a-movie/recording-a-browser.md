# Recording a browser

`movie browse` films a web app the way `movie term` films a shell. A
headless Chrome holds the page, and you drive it one action at a time with
your own tool calls, a sub-agent, or a script. A drawn cursor glides to
each click, typing happens at human pace, and `stop` renders what the page
showed and writes the scene file for `movie build`. It needs Chrome,
Chromium, or Edge, and runs on macOS, Linux, and WSL.

```bash
m="$SKILL_DIR/bin/movie"
"$m" browse start demo/ http://localhost:3000/ --title "Todo" --subtitle "adding an item, proven on camera"
"$m" browse type demo/ "text=What needs doing?" "buy milk"
"$m" browse click demo/ text=Add
"$m" browse wait demo/ "text=Saved" --say "We add an item, and the app saves it."
"$m" browse stop demo/ demo/takes/          # renders the takes, writes demo/takes/scenes.yaml
"$m" build demo/takes/scenes.yaml todo.mp4
```

Every action prints what it acted on (`clicked <button> "Add"`) and where
the page ended up (`at URL "title"`). `page` prints the page's visible text
and every link, button, and field, each with a TARGET that names it and a
field's current value. Run it whenever you need to know what to click.

**Record against a copy of the app's data.** A demo writes: it creates
records, saves edits, fires jobs. Serve a scratch copy, never the data you
care about and never production.

**Targets.** A TARGET is a CSS selector (`#item`, `form button`) or
`text=Label`, where Label is what a person would call the element: a
button's or link's text, a field's label or placeholder. To act on one,
buttons, links, and fields come first; `wait` looks at any element showing
the text. Matching ignores case and extra spaces, and exact matches win
over whole-word ones: `text=Saved` finds "Saved 2" but never "Unsaved
changes". Check what an action printed it found. Between matches, one a
click can reach wins over one covered by a modal. If nothing matches, the
verb exits 1 and lists the page's targets. If the target is covered, it
exits 1 and names what covers it. It does not click through.

**`--say` is how you narrate.** It works on `goto`, `click`, `type`,
`press`, and `wait`, and it ends a beat: the sentence narrates everything
since the previous `--say` and plays from the beat's start, over the
actions it describes. Put it on the action whose result proves the point.
That is usually the `wait` for the result, not the click that asked for
it. Above, on the `click`, the beat would end before the page saved. Keep
beats to an action or two, so the words play while it happens: a long
beat is silent once its sentence ends. `--say` needs filming on; with it
off, nothing would show what the sentence describes.

**`stop` writes the scene file**: the title card, then one scene per beat
with its sentence, starting with the beat. When a long beat's sentence is
about a result that only appears at its end, add `narration_at: end` to
that scene, so the words land on the result. It is an ordinary scene file
(assembling.md), so edit it before you build.

Time the page spends waiting for your next action, holding still, is cut to
1.5 seconds, so pausing to think puts no dead air in the movie. A page that
keeps animating (a spinner, a carousel) is not holding still, and that time
stays. Time the app spends working after an action is kept. So is a failed
action's: a `wait` that times out leaves nothing in the movie.

## The verbs

| Verb | Does | Exit |
|---|---|---|
| `start SESSION URL [--size 1280x720] [--title T] [--subtitle S] [--browser PATH]` | launch the browser, start filming, load URL, and return once it settles | 0, or 2 |
| `goto SESSION URL` | load URL | 0, 1 it would not load |
| `click SESSION TARGET` | glide the cursor to TARGET and click it | 0, 1 missing or covered |
| `type SESSION TARGET 'text' [--replace]` | click TARGET, then type after what it holds, or over it with `--replace`; a newline presses Enter | 0, 1 |
| `choose SESSION TARGET 'Option'` | pick the option labelled Option in the select TARGET | 0, 1 |
| `press SESSION KEY` | `Enter`, `Tab`, `Escape`, `Backspace`, `Delete`, `Home`, `End`, `Up`, `Down`, `Left`, `Right`, or one character | 0 |
| `wait SESSION TARGET [--timeout 10]` | wait until TARGET is visible, and scroll it into view | 0, 1 timed out |
| `page SESSION` | print the URL, title, visible text, and targets | 0 |
| `cut SESSION` | end this take, holding its result, and start the next (`--say` does this for you) | 0 |
| `film SESSION on\|off` | keep what follows out of the movie; each `on` starts a new take | 0 |
| `stop SESSION OUTDIR` | close the browser, render every take, and write `OUTDIR/scenes.yaml` | 0 |
| `render SESSION OUTDIR` | render again from the recording, even after stop | 0 |

After every action the verb waits for the page to settle: loaded, and no
change for 0.4 seconds after the action, for at most 5 seconds. Use `wait`
for anything slower.
Session and output directories must be new or empty.

The viewport is 1280x720 CSS pixels, filmed at 1600x900. An app designed
for a wide screen may need `--size 1440x900`, but a larger viewport means
smaller text in the movie. Check the contact sheet for legibility.

## When the correct behavior is invisible

Some claims are proven by nothing changing: state survives a reload, a
retry is idempotent. Filmed naively, the before and after look identical
and the movie shows nothing. Film the event, not the effect: `goto
about:blank` and back rather than reloading in place, so there is a real
teardown on camera, then the restored state. Keep such a beat on screen
more than a second: `movie check` samples once a second.

## Long work does not belong inside one take

A build, a deploy, a model generating: `film off`, `wait` for the result
(with a long `--timeout`), `film on`, then `wait` for it again with
`--say`, which returns at once and narrates the result on camera. The movie
cuts from the request to its result, and a card in the scene file can say
how long it took.

## When browse is not enough

Drag and drop, file uploads, anything inside an iframe, multi-tab flows,
keyboard shortcuts with modifiers, and logins that need more than typing
into a form are beyond these verbs. Drive Chrome yourself
with Playwright or raw CDP, as recording-motion.md describes.
