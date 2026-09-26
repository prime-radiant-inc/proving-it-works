#!/usr/bin/env bash
# Film the tour: Claude using the skill to film a terminal, a web app, and a
# desktop calculator, on macOS or Linux. The outer terminal, the one the
# movie shows, is itself filmed with movie term; every command typed in it
# is real and makes the recording the movie then cuts to.
#
# Usage: examples/tour/film.sh mac|linux
#
# mac   runs here. It needs Chrome, tmux, ffmpeg, and, for the calculator,
#       Screen Recording and Accessibility permission for the terminal app.
#       Do not type while it films the calculator: keystrokes go to the
#       frontmost app.
# linux runs in the movie-tour container (see Dockerfile), started by this
#       script. The outer terminal lives in the container via docker exec.
#
# Output: out/PLATFORM/ holds the inner recordings, out/PLATFORM-me/ the
# outer session, and out/PLATFORM-me-takes/ its takes. scenes.yaml cuts them
# together. Re-running needs out/PLATFORM* moved away first.
set -euo pipefail
platform=${1:?usage: film.sh mac|linux}
here=$(cd "$(dirname "$0")" && pwd)
bin=$here/../../skills/proving-it-works-with-a-movie/bin
m=$bin/movie
work=$here/out/$platform
me=$here/out/$platform-me
mkdir -p "$work"

case $platform in
  linux)
    arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
    linuxbin=$(mktemp -d)
    bash "$here/../../script/build-binaries" "$linuxbin" "linux/$arch" > /dev/null
    docker rm -f tour > /dev/null 2>&1 || true
    docker run -d --name tour --shm-size 1g \
      -v "$work:/work" \
      -v "$linuxbin/movie-linux-$arch:/usr/local/bin/movie:ro" \
      -v "$here/app/todo:/usr/local/bin/todo:ro" \
      -v "$here/app/index.html:/srv/index.html:ro" \
      movie-tour > /dev/null
    docker exec -d tour Xvfb :99 -screen 0 1280x800x24
    docker exec -d -w /srv tour python3 -m http.server 8000
    "$m" term start "$me" --cwd /work --size 100x26 -- docker exec tour
    ;;
  mac)
    cp "$here/app/index.html" "$work/"
    (cd "$work" && exec python3 -m http.server 8000 > /dev/null 2>&1) &
    server=$!
    trap 'kill $server' EXIT
    "$m" term start "$me" --cwd "$work" --size 100x26
    "$m" term film "$me" off
    # the screen's capture device, and device pixels per point (Retina is 2)
    # (listing devices always exits nonzero, hence || true)
    screen=$( (ffmpeg -hide_banner -f avfoundation -list_devices true -i "" 2>&1 || true) | sed -n 's/.*\[\([0-9]*\)\] Capture screen 0.*/\1/p')
    scale=$(system_profiler SPDisplaysDataType | grep -c Retina > /dev/null && echo 2 || echo 1)
    "$m" term run "$me" "export PATH=$bin:$here/app:\$PATH SCREEN=$screen SCALE=$scale; clear"
    "$m" term film "$me" on
    ;;
  *) echo "usage: film.sh mac|linux" >&2; exit 2 ;;
esac
sleep 1

# Each command is typed and run in the filmed terminal; with a sentence, it
# ends a narrated beat of the tour.
run() { "$m" term run "$me" "$1" --timeout 120 ${2:+--say "$2"}; }

# A terminal: the todo CLI, narrated beat by beat.
run "clear; movie term start t --size 56x16" "First, a terminal. I start a session, and movie term films it."
run "movie term run t 'todo add buy milk'" "I run the app's commands in it, one at a time."
run "movie term run t 'todo done 1' --say 'We add an item and mark it done.'" \
  "Each run prints what happened, and a sentence narrates the beat."
run "movie term stop t t-takes" "Stop renders the takes and writes a scene file, ready to build."

# A web app: the todo page, driven in headless Chrome.
run "clear; movie browse start b http://localhost:8000/ --size 960x540" \
  "Next, a web app. movie browse opens it in headless Chrome."
run "movie browse type b 'text=What needs doing?' 'buy milk'" "I type into a field by its label,"
run "movie browse click b text=Add" "click the button,"
run "movie browse wait b 'text=Saved 1' --say 'We add an item, and the app saves it.'" \
  "and wait for the result, with a sentence to narrate it."
run "movie browse stop b b-takes" "Stop renders what the browser showed, with a cursor drawn in."

# A desktop app: the calculator, captured with ffmpeg as the skill describes.
# The capture and the keystrokes share one command, so typing the next
# command never eats the capture's seconds.
case $platform in
  linux)
    run "clear; galculator 2> /dev/null &" "Last, a desktop app: the calculator."
    run 'eval $(xdotool search --sync --onlyvisible --name galculator getwindowgeometry --shell)' "I find its window,"
    run 'mkdir calc; ffmpeg -nostdin -v error -f x11grab -framerate 10 -video_size ${WIDTH}x${HEIGHT} -i :99+$X,$Y -t 5 calc/f%04d.png & sleep 1; xdotool windowfocus --sync $WINDOW type --delay 400 "12*34="; wait $!' \
      "and film it with ffmpeg while xdotool types a sum."
    ;;
  mac)
    run "clear; open -a Calculator" "Last, a desktop app: the calculator."
    run "eval \$(osascript -e 'tell application \"System Events\" to tell window 1 of process \"Calculator\" to set {{x, y}, {w, h}} to {position, size}' -e 'return \"X=\" & x & \" Y=\" & y & \" W=\" & w & \" H=\" & h')" \
      "I find its window,"
    # macOS's capture framework prints objc warnings ffmpeg cannot silence
    run 'mkdir calc; ffmpeg -nostdin -v error -f avfoundation -pixel_format bgr0 -framerate 10 -capture_cursor 1 -i "$SCREEN:none" -t 5 -vf "crop=$((SCALE*W)):$((SCALE*H)):$((SCALE*X)):$((SCALE*Y)),fps=10" calc/f%04d.png 2> /dev/null & sleep 1; osascript -e "tell application \"Calculator\" to activate" -e "repeat with c in characters of \"12*34=\"" -e "tell application \"System Events\" to keystroke c" -e "delay 0.4" -e "end repeat"; wait $!' \
      "and film it with ffmpeg while AppleScript types a sum."
    ;;
esac
"$m" term stop "$me" "$me-takes"
