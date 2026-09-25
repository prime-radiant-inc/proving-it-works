#!/usr/bin/env bash
# Film Blender, on Linux, building a snowman step by step (build.py drives
# its UI), with the skill's desktop route: ffmpeg captures the X display, and
# each step becomes its own frames scene so its narration lines up.
#
# Usage: examples/blender/film.sh   (after: docker build -t movie-blender examples/blender)
# Then:  movie build examples/blender/scenes.yaml examples/blender/out/snowman.mp4
# Output goes to out/, which must not exist yet.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
out=$here/out
if [[ -e $out ]]; then echo "$out exists: move it away first" >&2; exit 2; fi
mkdir -p "$out/capture"

docker rm -f blender-demo > /dev/null 2>&1 || true
docker run -d --name blender-demo -v "$out:/work" -v "$here/build.py:/build.py:ro" movie-blender > /dev/null
trap 'docker rm -f blender-demo > /dev/null' EXIT
docker exec -d blender-demo Xvfb :99 -screen 0 1600x900x24 +extension GLX
sleep 1

# Each frame is named by the wall-clock millisecond it was captured, the
# clock build.py stamps its steps with, so frames line up with steps.
docker exec -d blender-demo ffmpeg -nostdin -v error -use_wallclock_as_timestamps 1 \
  -f x11grab -draw_mouse 0 -framerate 10 -video_size 1600x900 -i :99 \
  -copyts -vf settb=1/1000 -fps_mode passthrough -frame_pts 1 -enc_time_base 1/1000 /work/capture/%d.png
docker exec blender-demo blender --window-geometry 0 0 1600 900 --python /build.py > "$out/blender.log" 2>&1
docker exec blender-demo pkill -INT ffmpeg
while docker exec blender-demo pgrep ffmpeg > /dev/null; do sleep 0.2; done

# One frames directory per step, from that step to the next.
python3 - "$out" <<'PY'
import json, os, shutil, sys
out = sys.argv[1]
frames = sorted((int(name[:-4]) / 1000, name) for name in os.listdir(f"{out}/capture"))
steps = [json.loads(l) for l in open(f"{out}/steps.jsonl")]
for step, nxt in zip(steps, steps[1:]):
    mine = [name for t, name in frames if step["t"] <= t < nxt["t"]]
    d = f"{out}/{step['step']}"
    os.makedirs(d)
    for i, name in enumerate(mine):
        shutil.copy(f"{out}/capture/{name}", f"{d}/f{i:05d}.png")
    print(f"{step['step']}: {len(mine)} frames")
PY
