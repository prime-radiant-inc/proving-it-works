#!/usr/bin/env bash
# Film the e2e demo's terminal beats inside the running container, from the
# host. The container needs tmux; movie and the recording stay out here.
# Safe to re-run: the take directories are regenerated each time, and a
# failed run stops its session so no tmux is left running in the container.
#
# Usage: examples/e2e/film.sh CONTAINER
set -euo pipefail
container=$1
here=$(cd "$(dirname "$0")" && pwd)
movie=$here/../../skills/proving-it-works-with-a-movie/bin/movie
work=$here/takes-$(date +%s)
wrap=(-- docker exec "$container")

# The take directories are generated output, and term stop needs them empty.
for take in install agent verify; do
  rm -rf "${here:?}/$take"
  mkdir "$here/$take"
done

# The session in progress, if any. On any exit, stop it: stop kills the
# container's tmux server before it renders, so a failed render is harmless.
current=
stop_current() {
  if [[ -n $current ]]; then
    "$movie" term stop "$current" "$current-aborted" > /dev/null 2>&1 || true
  fi
}
trap stop_current EXIT

start() { "$movie" term start "$1" --cwd /work --size 125x34 "${wrap[@]}"; current=$1; }
stop() { "$movie" term stop "$1" "$2"; current=; }

start "$work/install"
"$movie" term run "$work/install" 'claude plugin list'
"$movie" term run "$work/install" 'claude plugin marketplace add prime-radiant-inc/proving-it-works' --timeout 120
"$movie" term run "$work/install" 'claude plugin install proving-it-works' --timeout 120
"$movie" term run "$work/install" 'claude plugin list'
stop "$work/install" "$here/install"

start "$work/agent"
"$movie" term run "$work/agent" 'claude --permission-mode bypassPermissions -p "Use the proving-it-works-with-a-movie skill. Make a ~12s NARRATED movie proving /work/app/index.html counts 0 to 1 to 2 when clicked. Film the clicks with movie browse. No API key here, so narrate with the local engine. Build it with movie build and check it. Save to /work/out/counter.mp4."' --timeout 20 || true
"$movie" term film "$work/agent" off
"$movie" term wait "$work/agent" --timeout 1800
stop "$work/agent" "$here/agent"

start "$work/verify"
"$movie" term run "$work/verify" 'ls -la out/counter.mp4'
"$movie" term run "$work/verify" 'SKILL=$(dirname "$(find ~/.claude/plugins -path "*proving-it-works-with-a-movie*" -name SKILL.md | head -1)")'
"$movie" term run "$work/verify" '"$SKILL/bin/movie" check out/counter.mp4' --timeout 120
stop "$work/verify" "$here/verify"
