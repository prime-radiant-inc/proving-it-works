# Recording motion yourself

For a web app, start with `movie browse` (recording-a-browser.md). It
draws the cursor, paces the typing, cuts the waiting, and writes the scene
file. This file is for what it cannot do: drag and drop, uploads, iframes,
multi-tab flows, and complex logins, where you drive Chrome yourself with
Playwright or raw CDP, and for capturing a desktop app's window.

## Record against a copy, always

A demo movie *writes*: it creates records, saves edits, fires jobs. Copy the
data tree to a scratch suite and serve that. Never point the recorder at the
tree you care about, and never at a production instance.

## Two capture styles

**Native video capture** (Playwright `record_video_dir`, Chrome DevTools
screencast) gives you a continuous clip for free. Playwright needs its own
bundled encoder — `playwright install ffmpeg` — separate from system ffmpeg.
Good when you want one continuous take.

**Deliberate frame capture** (screenshot per beat, encode at a chosen rate)
costs more code and buys per-beat control over pacing, which is what you
need when narration has to line up. This is the right default for a narrated
tutorial.

## Draw a cursor or the app appears haunted

Browser automation moves an invisible pointer: a click looks like the UI
changing by itself, which is exactly what a skeptical reviewer discounts.
Inject a cursor overlay on every page and animate it to each target before
clicking, with a press pulse on mousedown.

```js
// injected via addInitScript / Page.addScriptToEvaluateOnNewDocument
const ring = document.createElement("div");
ring.style.cssText = "position:fixed;width:20px;height:20px;border:3px solid " +
  "rgba(255,64,129,.9);border-radius:50%;pointer-events:none;z-index:2147483647;" +
  "transform:translate(-50%,-50%);transition:transform .08s";
document.addEventListener("DOMContentLoaded", () => document.body.appendChild(ring));
document.addEventListener("mousemove", e => {
  ring.style.left = e.clientX + "px"; ring.style.top = e.clientY + "px";
}, true);
document.addEventListener("mousedown",
  () => ring.style.transform = "translate(-50%,-50%) scale(.6)", true);
document.addEventListener("mouseup",
  () => ring.style.transform = "translate(-50%,-50%)", true);
```

Type at human pace too (~55ms/char, longer after punctuation). Instant text
insertion reads as a scripted fake even when it isn't.

## Describe scenes as data, not code

Put the movie in a scene list — id, narration, ordered actions — and keep the
recorder generic. You will re-record individual scenes many times; editing a
YAML entry beats editing a script every time. Verbs worth having:
`goto`, `wait_for`, `click`, `type`, `append` (caret to end, then type),
`select`, `pause`.

Check your scene list against the recorder's actual verbs *before* a long
pass. A verb the recorder doesn't implement fails at record time, after
you've spent the wall clock.

## Only type into empty fields

Automation appends at whatever caret exists. To edit existing text you need
an explicit caret move (`ControlOrMeta+ArrowDown` to end, then type).
Anything else silently produces mangled input on camera.

## When the correct behavior is invisible

Film the event, not the effect, and hold it more than a second:
recording-a-browser.md explains.

## Screenshot-based capture: navigation orphans an in-flight capture

Driving CDP directly, a `Page.captureScreenshot` issued as a navigation
begins never gets a reply — not slowly, *never*. A capture loop that awaits
it hangs until whatever global timeout you have expires.

Race every capture against a short timeout (~700ms) and skip the frame:

```js
const shot = await Promise.race([
  send("Page.captureScreenshot", { format: "png" }),
  new Promise(r => setTimeout(() => r(null), 700)),
]);
if (shot) writeFrame(shot.data);   // dropped frames are fine; a hung loop is not
```

Write each frame as a numbered PNG into one directory; that directory is a
`frames` scene.

## Slow real work does not fit inside a scene

A genuine multi-minute operation (a model generating, a build, a deploy)
cannot be waited out inside a recording pass — and if the recorder owns the
server, shutting it down at end-of-pass kills the job mid-flight and leaves
half-written artifacts.

Split into passes: record up to the trigger, let the pass end, produce the
artifact off-camera with the normal CLI, then record the pass that opens the
finished result. The movie is honest — the work really happened — and no
scene depends on a job outliving the process that started it.

## App-specific gotchas worth checking before a pass

- **Auth in the URL**: apps that read a token from `?k=` on first load and
  scrub it need the token on the *first* navigation of each fresh context
  only; tagging every navigation forces reloads and breaks hash routing.
- **Typed fields with parsers**: a value like `Yes`/`No`/`On`/`Off` in a
  YAML-backed form field saves as a boolean and can crash the app on camera.

## Capturing a desktop app's window

One ffmpeg command per OS captures a window or the screen into frames that
`movie build` takes as a `frames` scene. First capture two seconds and look
at a still: when screen-recording permission is missing, capture
"succeeds" and records wallpaper.

```bash
# macOS: list devices, then capture screen N (grant Screen Recording to your terminal)
ffmpeg -f avfoundation -list_devices true -i ""
mkdir -p check
ffmpeg -nostdin -f avfoundation -framerate 10 -capture_cursor 1 -i 'N:none' -t 2 -vf fps=10 check/f%04d.png

# Linux (X11)
mkdir -p check
ffmpeg -nostdin -f x11grab -framerate 10 -i "$DISPLAY" -t 2 -vf fps=10 check/f%04d.png
```

```powershell
# Windows: one window by its exact title
New-Item -ItemType Directory -Force check | Out-Null
ffmpeg -nostdin -f gdigrab -framerate 10 -i 'title=Your application window title' -t 2 -vf fps=10 check/f%04d.png
```

Look at `check/f0010.png`. Only visible application pixels are a capture;
wallpaper or a blank window means permission is blocked. Then create a new
directory for the real take and use it as `frames: that-directory`,
`rate: 10`. If no capture shows the app, use the browser route or a reel
rendered from the log, and say what remains unproven.
