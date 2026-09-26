# A Linux desktop for `movie desk`: an Xvfb display on :99 and the tools
# desk runs on the display's side. Add your app's Debian packages with APPS:
#
#   docker build -t movie-desktop --build-arg APPS="galculator" "$SKILL_DIR/examples/linux-desktop"
#   docker run -d --init --name app-box movie-desktop
#   docker exec -d app-box galculator
#
# The display is 1280x720; for another size, end the run line with
# Xvfb :99 -screen 0 1600x900x24 +extension GLX
FROM debian:trixie

RUN apt-get update && apt-get install -y --no-install-recommends \
      xvfb xdotool ffmpeg x11-utils procps fonts-dejavu-core

# The app goes in its own layer, so the tools above stay cached between apps.
ARG APPS=""
RUN if [ -n "$APPS" ]; then apt-get update && apt-get install -y --no-install-recommends $APPS; fi

ENV DISPLAY=:99
CMD ["Xvfb", ":99", "-screen", "0", "1280x720x24", "+extension", "GLX"]
