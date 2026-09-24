#!/usr/bin/env bash
# Film the e2e demo's terminal beats inside the running container, from the
# host. The container needs tmux; movie and the recording stay out here.
#
# Usage: examples/e2e/film.sh CONTAINER
set -euo pipefail
container=$1
here=$(cd "$(dirname "$0")" && pwd)
movie=$here/../../skills/proving-it-works-with-a-movie/bin/movie
work=$here/takes-$(date +%s)
wrap=(-- docker exec "$container")

"$movie" term start "$work/install" --cwd /work --size 125x34 "${wrap[@]}"
"$movie" term run "$work/install" 'claude plugin list'
"$movie" term run "$work/install" 'claude plugin marketplace add prime-radiant-inc/proving-it-works' --timeout 120
"$movie" term run "$work/install" 'claude plugin install proving-it-works' --timeout 120
"$movie" term run "$work/install" 'claude plugin list'
"$movie" term stop "$work/install" "$here/install"

"$movie" term start "$work/agent" --cwd /work --size 125x34 "${wrap[@]}"
"$movie" term run "$work/agent" 'claude --permission-mode bypassPermissions -p "Use the proving-it-works-with-a-movie skill. Make a ~12s NARRATED movie proving /work/app/index.html counts 0 to 1 to 2 when clicked. Motion route: record continuous frames from headless chromium with the cursor overlay drawn. No API key here, so narrate with the local engine. Build it with movie build and check it. Save to /work/out/counter.mp4."' --timeout 20 || true
"$movie" term film "$work/agent" off
"$movie" term wait "$work/agent" --timeout 1800
"$movie" term stop "$work/agent" "$here/agent"

"$movie" term start "$work/verify" --cwd /work --size 125x34 "${wrap[@]}"
"$movie" term run "$work/verify" 'ls -la out/counter.mp4'
"$movie" term run "$work/verify" 'SKILL=$(dirname "$(find ~/.claude/plugins -path "*proving-it-works-with-a-movie*" -name SKILL.md | head -1)")'
"$movie" term run "$work/verify" '"$SKILL/bin/movie" check out/counter.mp4' --timeout 120
"$movie" term stop "$work/verify" "$here/verify"
