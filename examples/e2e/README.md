# The plugin's own end-to-end proof

What `docs/demo.mp4` is a recording of: a clean container installs this
plugin from the public marketplace, an agent inside it uses the skill to
make a narrated movie of a small web app, and the skill's own checker
verifies that movie. Nothing of ours is baked into the image — the plugin
arrives the way a stranger's would.

This is a demonstration, not a CI test: it needs Docker and a Claude Code
credential, and it costs several minutes of real agent work. Nothing here
runs unattended.

## Pieces

| File | What it is |
|---|---|
| `Dockerfile` | Debian + Node + Claude Code + ffmpeg + chromium + tmux/ttyd + mpv, and a pre-cached Piper voice so the container can narrate with no API key |
| `app/index.html` | the subject: a counter that increments on click |
| `scenes.yaml` | the demo's own scene script, assembled with `scripts/assemble` |
| `../film-terminal.py` | drives the container's tmux session and screenshots it (see `recording-a-terminal.md`) |

## Running it

```bash
docker build -t proving-e2e .
docker run -d --name provdemo -e CLAUDE_CODE_OAUTH_TOKEN \
  -v "$PWD/work:/work" -p 7100:7681 proving-e2e bash -lc \
  'tmux new-session -d -s demo -x 125 -y 34
   exec ttyd -p 7681 -t fontSize=17 -t "fontFamily=DejaVu Sans Mono,monospace" \
        tmux attach -t demo'

./../film-terminal.py install http://127.0.0.1:7100/ provdemo
./../film-terminal.py agent   http://127.0.0.1:7100/ provdemo   # starts the agent
# wait for the shell to come back, then:
./../film-terminal.py verify  http://127.0.0.1:7100/ provdemo
```

Then narrate, assemble, subtitle, and gate it with the skill's own scripts:

```bash
SKILL=../../skills/proving-it-works-with-a-movie/scripts
$SKILL/narrate        scenes.yaml narration/
$SKILL/assemble       scenes.yaml silent-cut.mp4
$SKILL/make-subtitles narration/manifest.json demo.srt \
                      --offsets-json segments/offsets.json
$SKILL/burn-subtitles silent-cut.mp4 demo.srt demo.mp4
$SKILL/check-movie    demo.mp4
```

## What it cost to get right

Every one of these was a silent failure — no error, just a wrong movie:

- Headless Chrome renders ttyd's canvas blank without software GL. 73 blank
  frames before anyone noticed; there is a preflight for it now.
- Keystrokes sent while the agent was still running went into *its* stdin
  and were echoed as text, producing a take of commands that never ran.
- The agent's first movie crammed every visible change into three seconds
  and then froze while the narration kept going — caught by `check-movie`,
  which is the reason it exists.
- Homebrew's ffmpeg has no libass, so subtitles could not be burned on the
  host at all; the container's Debian ffmpeg can. `burn-subtitles` detects
  this rather than silently producing a movie with no visible subtitles.
