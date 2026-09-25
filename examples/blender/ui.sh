#!/usr/bin/env bash
# Use Blender's UI by hand, on Linux, while it is filmed: the desktop-app
# route with the same shape as movie term and movie browse. Look with shot,
# act with move/click/key/type, and narrate with say, which ends a beat.
#
#   ui.sh start                 container, Xvfb, Blender, continuous capture
#   ui.sh shot                  save the screen to out/shot.png, to look at
#   ui.sh move X Y              glide the pointer to X,Y (screen pixels)
#   ui.sh click X Y [right]     glide there and click
#   ui.sh drag X1 Y1 X2 Y2      press at X1,Y1, glide to X2,Y2, release
#   ui.sh key KEY...            press keys, xdotool names: shift+a, Tab, Return
#   ui.sh type TEXT             type text at human pace
#   ui.sh say "sentence"        end a beat: the sentence narrates everything
#                               since the last say, over this beat's result
#   ui.sh stop                  stop filming, then cut
#   ui.sh cut                   write out/beat-N/ and out/scenes.yaml from the capture
#
# Time spent waiting on the next action with the screen unchanged is cut to
# 1.5 s at stop, so thinking between actions leaves no dead air. Needs the
# movie-blender image: docker build -t movie-blender examples/blender
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
out=$here/out
c=blender-ui
x() { docker exec "$c" "$@"; }

# glide moves the pointer to $1,$2 in small steps, so the movie shows it travel.
glide() {
  x bash -c '
    eval $(xdotool getmouselocation --shell)
    for i in $(seq 1 15); do
      xdotool mousemove $((X + ($1 - X) * i / 15)) $((Y + ($2 - Y) * i / 15)); sleep 0.025
    done' _ "$1" "$2"
}

case ${1:-} in
  start)
    if [[ -e $out ]]; then echo "$out exists: move it away first" >&2; exit 2; fi
    mkdir -p "$out/capture"
    docker rm -f "$c" > /dev/null 2>&1 || true
    # --init reaps the processes started below once they exit
    docker run -d --init --name "$c" -v "$out:/work" movie-blender > /dev/null
    x bash -c 'Xvfb :99 -screen 0 1600x900x24 +extension GLX > /dev/null 2>&1 &'
    sleep 1
    x bash -c 'blender --window-geometry 0 0 1600 900 > /work/blender.log 2>&1 &'
    # each frame is named by the wall-clock millisecond it was captured, so
    # say's timestamps cut the capture exactly
    x bash -c 'ffmpeg -nostdin -v error -use_wallclock_as_timestamps 1 -f x11grab -draw_mouse 1 \
      -framerate 10 -video_size 1600x900 -i :99 -copyts -vf settb=1/1000 -fps_mode passthrough \
      -frame_pts 1 -enc_time_base 1/1000 /work/capture/%d.png > /work/ffmpeg.log 2>&1 &'
    sleep 8
    x xdotool mousemove 800 450
    echo "filming; look with: ui.sh shot"
    ;;
  shot) x ffmpeg -nostdin -v error -y -f x11grab -draw_mouse 1 -video_size 1600x900 -i :99 -frames:v 1 /work/shot.png
        echo "$out/shot.png" ;;
  move) glide "$2" "$3" ;;
  # Blender ignores a click that arrives the instant the pointer does, and
  # buttons in its panels need the press held a moment.
  click) glide "$2" "$3"; b=$([[ ${4:-} == right ]] && echo 3 || echo 1)
         x bash -c "sleep 0.2; xdotool mousedown $b; sleep 0.12; xdotool mouseup $b" ;;
  drag) glide "$2" "$3"; x xdotool mousedown 1; glide "$4" "$5"; x xdotool mouseup 1 ;;
  key) shift; for k in "$@"; do x xdotool key "$k"; sleep 0.25; done ;;
  type) x xdotool type --delay 80 "$2" ;;
  say) sleep 1  # let the result land on screen before the beat ends
       x python3 -c 'import json,sys,time; open("/work/beats.jsonl","a").write(json.dumps({"t": time.time(), "say": sys.argv[1]})+"\n")' "$2" ;;
  stop)
    x pkill -INT ffmpeg || true
    while x pgrep -r R,S,D ffmpeg > /dev/null; do sleep 0.2; done  # a zombie has finished
    docker rm -f "$c" > /dev/null
    exec "$0" cut
    ;;
  cut)
    python3 - "$out" <<'PY'
import hashlib, json, os, shutil, sys
out = sys.argv[1]
FPS, HOLD = 10, 1.5
# the capture drops frames under load; each frame's name is when it was
# taken, so frames are placed by time, each shown until the next
frames = sorted((int(n[:-4]) / 1000, n) for n in os.listdir(f"{out}/capture"))
beats = [json.loads(l) for l in open(f"{out}/beats.jsonl")]
bounds = [frames[0][0]] + [b["t"] for b in beats] + [frames[-1][0] + 0.1]
look = lambda n: hashlib.sha1(open(f"{out}/capture/{n}", "rb").read()).digest()
scenes = []
for i, (lo, hi) in enumerate(zip(bounds, bounds[1:]), 1):
    mine = [(t, n) for t, n in frames if lo <= t < hi]
    if not mine:
        continue
    # each frame's time on screen, with any stretch of an unchanging picture
    # cut to HOLD seconds: the time spent deciding what to do next
    shown, waited, last = [], 0.0, None
    for k, (t, n) in enumerate(mine):
        span = (mine[k + 1][0] if k + 1 < len(mine) else hi) - t
        h = look(n)
        if h == last:
            keep = min(span, max(0.0, HOLD - waited))
            waited += keep
        else:
            keep = waited = min(span, HOLD)
        last = h
        shown.append((n, keep))
    # resample at FPS
    d = f"{out}/beat-{i}"
    os.makedirs(d)
    k, written = 0, 0
    total = sum(dur for _, dur in shown)
    ends, acc = [], 0.0
    for n, dur in shown:
        acc += dur
        ends.append(acc)
    while written / FPS < total:
        at = written / FPS
        while k < len(shown) - 1 and ends[k] <= at:
            k += 1
        shutil.copy(f"{out}/capture/{shown[k][0]}", f"{d}/f{written:05d}.png")
        written += 1
    say = beats[i - 1]["say"] if i <= len(beats) else ""
    scenes.append((f"beat-{i}", say))
    print(f"beat-{i}: {total:.1f}s ({hi - lo:.1f}s filmed)  {say}")
with open(f"{out}/scenes.yaml", "w") as f:
    f.write("# Written by ui.sh stop. Edit freely, then: movie build scenes.yaml OUT.mp4\nsize: 1600x900\nscenes:\n")
    for sid, say in scenes:
        f.write(f"  - id: {sid}\n    frames: {sid}\n    rate: {FPS}\n    narration_at: end\n")
        if say:
            f.write(f"    narration: {json.dumps(say)}\n")
print(f"wrote {out}/scenes.yaml")
PY
    ;;
  *) sed -n '2,20p' "$0"; exit 2 ;;
esac
