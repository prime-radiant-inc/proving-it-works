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

Every action prints where the page ended up (`at URL "title"`). `page`
prints the page's visible text and every link, button, and field, each with
a TARGET that names it. Run it whenever you need to know what to click.

**Record against a copy of the app's data.** A demo writes: it creates
records, saves edits, fires jobs. Serve a scratch copy, never the data you
care about and never production.

**Targets.** A TARGET is a CSS selector (`#item`, `form button`) or
`text=Label`, where Label is what a person would call the element: a
button's or link's text, a field's label or placeholder. Matching ignores
case and extra spaces and prefers exact matches over partial ones. If
nothing matches, the verb exits 1 and lists the page's targets. If the
target is covered by something else, such as a modal or a cookie banner, it
exits 1 and names what covers it. It does not click through.

**`--say` is how you narrate.** It works on `goto`, `click`, `type`,
`press`, and `wait`, and it ends a beat: the sentence narrates everything
since the previous `--say` and plays over this action's result. Put it on
the action whose result proves the point. That is usually the `wait` for
the result, not the click that asked for it. Above, the sentence would play
over a page still saving if it were on the `click`.

**`stop` writes the scene file**: the title card, then one scene per beat
with its sentence, set to play over the beat's result. It is an ordinary
scene file (assembling.md), so edit it before you build.

Time the page spends waiting for your next action is cut to 1.5 seconds,
so pausing to think never puts dead air in the movie. Time the app spends
working after an action is kept.

## The verbs

| Verb | Does | Exit |
|---|---|---|
| `start SESSION URL [--size 1280x720] [--title T] [--subtitle S] [--browser PATH]` | launch the browser, start filming, load URL, and return once it settles | 0, or 2 |
| `goto SESSION URL` | load URL | 0, 1 it would not load |
| `click SESSION TARGET` | glide the cursor to TARGET and click it | 0, 1 missing or covered |
| `type SESSION TARGET 'text'` | click TARGET, then type; a newline presses Enter | 0, 1 |
| `press SESSION KEY` | `Enter`, `Tab`, `Escape`, `Backspace`, `Delete`, `Up`, `Down`, `Left`, `Right`, or one character | 0 |
| `wait SESSION TARGET [--timeout 10]` | wait until TARGET is visible | 0, 1 timed out |
| `page SESSION` | print the URL, title, visible text, and targets | 0 |
| `cut SESSION` | end this take, holding its result, and start the next (`--say` does this for you) | 0 |
| `film SESSION on\|off` | keep what follows out of the movie; each `on` starts a new take | 0 |
| `stop SESSION OUTDIR` | close the browser, render every take, and write `OUTDIR/scenes.yaml` | 0 |
| `render SESSION OUTDIR` | render again from the recording, even after stop | 0 |

After every action the verb waits for the page to settle: loaded, and no
change for 0.4 seconds, for at most 5 seconds. Use `wait` for anything slower.
Session and output directories must be new or empty.

`type` adds to whatever the field holds. Type into empty fields, or clear
one first (`press SESSION Backspace` for each character), or the take shows
mangled text.

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
(with a long `--timeout`), `film on`, and film the result. The movie cuts
from the request to its result, and a card in the scene file can say how
long it took.

## When browse is not enough

Drag and drop, file uploads, iframes, multi-tab flows, and logins that need
more than typing into a form are beyond these verbs. Drive Chrome yourself
with Playwright or raw CDP, as recording-motion.md describes.
