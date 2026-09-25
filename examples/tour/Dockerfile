# The Linux half of the tour: a machine with a terminal, a browser, and a
# desktop app to film. Xvfb is the display, galculator the desktop app,
# xdotool the hands. movie itself is mounted in from the skill's bin/.
FROM debian:trixie

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates tmux chromium python3 ffmpeg xvfb xdotool galculator \
      fonts-dejavu-core procps

ENV TERM=xterm-256color DISPLAY=:99
WORKDIR /work
CMD ["sleep", "infinity"]
