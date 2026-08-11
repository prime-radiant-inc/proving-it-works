# Clean-room environment for the proving-it-works e2e: a stranger's machine
# with nothing of ours preinstalled. Claude Code installs the plugin from the
# public marketplace, then uses the skill to make a movie.
FROM node:22-bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl git ffmpeg tmux python3 python3-venv mpv \
      fonts-dejavu-core chromium \
    && rm -rf /var/lib/apt/lists/*

# ttyd isn't packaged for bookworm; serves the tmux session over HTTP so the
# terminal can be filmed as real pixels from outside the container
RUN arch="$(dpkg --print-architecture)" \
    && case "$arch" in arm64) t=aarch64 ;; amd64) t=x86_64 ;; *) echo "arch $arch" >&2; exit 1 ;; esac \
    && curl -fsSL -o /usr/local/bin/ttyd \
       "https://github.com/tsl0922/ttyd/releases/download/1.7.7/ttyd.${t}" \
    && chmod +x /usr/local/bin/ttyd && ttyd --version

# uv: check-movie runs via `uv run --script` (PEP 723 inline deps)
RUN curl -LsSf https://astral.sh/uv/install.sh | sh
ENV PATH="/root/.local/bin:${PATH}"

RUN npm install -g @anthropic-ai/claude-code

# Chromium for the skill's browser route; Playwright would re-download one,
# so point anything that asks at the system binary.
ENV CHROME_PATH=/usr/bin/chromium \
    PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium \
    TERM=xterm-256color \
    PATH="/root/.local/bin:${PATH}"

WORKDIR /work
CMD ["bash"]

# Claude Code refuses --dangerously-skip-permissions as root, so the demo
# runs as an unprivileged user (also closer to a real user's machine).
RUN useradd -m -s /bin/bash demo && mkdir -p /work && chown -R demo:demo /work
USER demo
ENV HOME=/home/demo PATH="/home/demo/.local/bin:/root/.local/bin:${PATH}"
RUN curl -LsSf https://astral.sh/uv/install.sh | sh
WORKDIR /work

# Pre-warm the local voice so the demo doesn't spend its first minute on a
# 60MB download. narrate finds it here via PIPER_VOICE_DIR.
ENV PIPER_VOICE_DIR=/home/demo/.cache/piper-voices
RUN uv run --quiet --with piper-tts python3 -c "\
from piper.download_voices import download_voice; import pathlib, os; \
d=pathlib.Path(os.environ['PIPER_VOICE_DIR']); d.mkdir(parents=True, exist_ok=True); \
download_voice('en_US-lessac-medium', d); print('voice cached')"
