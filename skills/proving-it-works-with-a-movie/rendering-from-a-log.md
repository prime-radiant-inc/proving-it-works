# Rendering a reel from the run's own log

For when there are no pixels to capture — OS screen recording is blocked, or
the thing to prove is a *run* (a test suite, a deploy, a job) rather than a
UI. Render an auditable reel from the real run's log instead of fighting the
OS for a picture.

Adapted from `recording-a-proof-movie.md` in obra/superpowers PR #1931.

## First: try real capture, and refuse to fake it

```bash
ffmpeg -f avfoundation -list_devices true -i ""      # probe devices

ffmpeg -y -hide_banner -f avfoundation -framerate 15 -capture_cursor 1 \
  -t 2 -i '<screen-index>:none' -vf scale=1280:-2 -pix_fmt yuv420p /tmp/cap-check.mp4
ffmpeg -y -hide_banner -i /tmp/cap-check.mp4 -frames:v 1 /tmp/cap-check.png
```

Look at that PNG. If it is wallpaper with no app window, Screen Recording
permission is denied for this process and capture will "succeed" while
recording nothing. **Do not ship it.** Say plainly that the OS blocked
capture and switch to the reel below — that pivot is the honest outcome, not
a fallback to apologize for. (`screencapture -x` has the same limitation;
`screencapture -x -l <windowID>` can still grab one window if you can
resolve its CoreGraphics id.)

## Make the real run the evidence source

Wrap the actual command so its log carries machine-checkable markers. Use
`bash`, not `zsh` — zsh's read-only `$status` injects a spurious error after
a passing run and pollutes the evidence.

```bash
bash -o pipefail -c '
  {
    printf "RUN_KIND=<name>\n";
    printf "STARTED_AT="; date -u +%Y-%m-%dT%H:%M:%SZ;
    <the real command>;
    rc=$?;
    printf "FINISHED_AT="; date -u +%Y-%m-%dT%H:%M:%SZ;
    printf "EXIT_STATUS=%s\n" "$rc"; exit "$rc"
  } 2>&1 | tee evidence/run.log
'
```

Keep each producer plus its `tee` under one `pipefail` owner, or a failing
command's status is lost and a failed run renders as a successful movie.

If the run touches a remote host or shared session, snapshot that state
identically before and after and diff them; equal snapshots prove the run
left no residue.

## Draw frames from the log

Render title / exact command / result / before-after diff / evidence-bundle
panels as images and write each one as a numbered PNG into one directory.
Keep it in a saved, re-runnable `generate_reel.py`, not a one-shot heredoc.

```python
from pathlib import Path
out = Path("frames/reel"); out.mkdir(parents=True, exist_ok=True)
index = 0
for nframes, render in scenes:                     # render(t) -> PIL RGB image
    for i in range(nframes):
        render(i / max(1, nframes - 1)).save(out / f"f{index:05d}.png")
        index += 1
```

Then use `frames: frames/reel` with the rate you rendered at, in a scene
file for `movie build`.

## Hash the bundle

The reel is *derived from* the log and snapshots; they ship next to it, not
instead of it.

```bash
shasum -a 256 reel.mp4 reel-check/contact-sheet.png run.log > SHA256SUMS
shasum -a 256 -c SHA256SUMS
```

Fix anything the movie renders — a timestamp, a log line, a stale selector —
and you regenerate the movie and re-hash. A hash that no longer matches the
log is a lie.

## Gate it

`movie build` runs the gate on the reel. A silent reel with no narration is
checked as silent and passes; add narration to explain it. Then open the
contact sheet and confirm the panels are legible at full size: a reel
nobody can read proves nothing.
