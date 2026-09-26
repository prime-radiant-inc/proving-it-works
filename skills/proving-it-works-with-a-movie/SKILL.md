---
name: proving-it-works-with-a-movie
description: Use when asked for a demo, screencast, tutorial, walkthrough, or proof video of software actually running, when a reviewer needs to see a feature work rather than take your word for it, or when handing over any video artifact of app behavior
---

# Proving It Works With a Movie

## Overview

A movie is evidence. Every way it fails is silent: no crash, no red text,
just an artifact that looks fine to whoever made it and is obviously broken
to the first person who watches it.

**Core principle: you have not made a movie until you have looked at the
movie.** Not the frames going in. The finished file coming out.

**Film with this skill's `movie` tool, not with browser or screen tools you
already have** (a Chrome extension, a Playwright MCP server, a GIF
recorder, computer-use screenshots). Those drive the user's own browser,
with their tabs and logins, and produce nothing `movie build` can narrate
or check. Pick the route below.

## Show each claim through its users' interface

Break the claim into parts and prove each one where its users would meet
it: the terminal for a CLI, the UI for an app, code calling it for a
library, the test runner for "the tests pass." Scripting that interface is
fine, and it makes the movie rerunnable: a doubtful reviewer can run it
again. Going around it (the app's scripting API, an internal endpoint, a
test hook, a database write) proves the way around works, not the thing
you built. To show that someone can model in Blender, click through
Blender's menus; building the model with `bpy` calls proves only that
Blender runs Python.

When the interface is hard to script (a 3D viewport, a canvas, a desktop
app with no automation hooks), use it by hand in a loop: look at the
screen, do one thing, look again (`movie desk shot`, then one action). Then capture what worked as a script if
the movie needs to be rerun.

## Pick the route

| What you have to show | Route |
|---|---|
| A web app in use: typing, clicking, a list updating live | `movie browse` → recording-a-browser.md |
| Drag and drop, uploads, iframes, complex logins | Drive the browser yourself → recording-motion.md |
| A desktop app on Linux (X11, or Xvfb in a container) | `movie desk` → recording-a-desktop.md |
| A desktop app on macOS or Windows | Capture the window with ffmpeg → recording-motion.md |
| A CLI, a TUI, an install, a test run, an agent working | `movie term` → recording-a-terminal.md |
| A sequence of real states, motion optional | Screenshots as `image` scenes → rendering-stills.md |
| OS capture blocked (wallpaper-only frames), or the thing to prove is a *run*, not a UI | Frames rendered from the run's own log → rendering-from-a-log.md |

Every route ends in `movie build` (assembling.md) and its gate.

Stills are a legitimate movie. Reach for motion only when the *motion* is
the claim; it costs several times more to build and is where sync defects
live.

**Never** mock, stage, or reenact. If a beat can't be shown for real
(no credentials, no data, a 40-minute job), cut it and say why. A movie
that quietly fakes one beat is worthless as evidence for any beat.

## Build it, and gate it

Every route ends in the same two commands. `$SKILL_DIR` is this skill's own
directory (the "Base directory for this skill" printed when it loads;
installed as a plugin, `$CLAUDE_PLUGIN_ROOT/skills/proving-it-works-with-a-movie`).

```bash
"$SKILL_DIR/bin/movie" build scenes.yaml movie.mp4   # narrate, assemble, subtitle, burn, check
"$SKILL_DIR/bin/movie" check other.mp4               # the gate alone, for a movie made elsewhere
```

`check` expects narration and subtitles by default. For a movie made
elsewhere that is meant to be silent, pass `--no-expect-audio`; for one
that is meant to have no subtitles, `--no-expect-subtitles`. (`build`
sets both expectations for you from the scene file.)

On Windows PowerShell, run `& "$SKILL_DIR/bin/movie-windows-amd64.exe"` with
the same arguments. From Git Bash, `bin/movie` works; pass paths in Windows
form (`cygpath -m`).

`build` reads a scene file (assembling.md), narrates each narrated scene with
a cloud voice when `OPENAI_API_KEY` exists and a local voice when it does
not (narrating.md), holds every scene for max(narration, visuals), writes
`movie.srt`, burns it into the picture, and runs the gate. A nonzero exit
means do not ship.

The gate samples picture and sound on one timeline and fails the movie when
the action is crammed into the first seconds while narration keeps talking,
when the picture never changes, when the audio is silent, or when a narrated
movie has no subtitles or subtitles that quit before the narration does. It
samples once a second, so any beat that must register (a flash, a blank
frame, a transition) has to be held longer than a second. Then:

1. **Open the contact sheet it wrote (`movie-check/contact-sheet.png`) and
   actually look at it.** Identical tiles mean a frozen movie. Unreadable
   text means your viewport is wrong.
2. **If narrated: listen to it.** No tool here can hear a mispronounced name
   or a skipped sentence. For `openai-chat`, the model's own transcript is
   gated, which proves what it says it said, not what is in the audio.
3. Fix, regenerate, re-run. Never patch the report instead of the movie.

## The silent failures

| What you get | Why it happens |
|---|---|
| Narrator talks over a picture that stopped moving | Sleeps guessed against narration nobody measured |
| A word missing from the narration | Local TTS drops out-of-vocabulary terms with no error |
| "Sure, here it is:" spoken aloud | Chat-model TTS ad-libs; it is not a TTS endpoint |
| Clicks that appear to happen by themselves | Automation draws no cursor (`movie browse` draws one) |
| Wallpaper, or a blank window | OS screen-recording permission denied; capture "succeeds" |
| A scene missing, error naming a truncated file | `ffmpeg` ate the loop's stdin (`-nostdin`) |
| Your real data mutated | You recorded against the live tree; the movie writes |
| Nothing visibly happens, because nothing visibly *should* | The claim is "state survived" — film the event, not the effect (recording-a-browser.md) |
| A muted viewer gets nothing | Narration without subtitles. `movie build` writes and burns them |

## Red flags — stop

- "The frames looked right" → frames are not a timeline. Run the checker.
- "ffprobe says 27 seconds" → duration is not content.
- "The TTS returned 200" → generation is not delivery. Listen to it.
- "I'll note the glitch in the handover" → regenerate it instead.
- "Close enough to demo" → you are about to hand a reviewer a frozen movie.
- "No API key, so no narration" → `movie build` uses a local voice.
- "I'll add subtitles later" → later is after someone watched it muted.
- "Its API is more reliable than clicking" → then the movie proves the API.
  Use the interface the claim is about.
- "I already have browser tools loaded" → they drive the user's own browser
  and make no narrated movie. Use `movie browse`.

## Keep the pipeline

The scene file, narration text, and whatever script drives the recording are
**committed files**, not scratch. Scratch directories get cleaned
mid-production and a movie you can't rebuild is a movie you can't fix. See
assembling.md.
