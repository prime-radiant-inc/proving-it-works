#!/usr/bin/env -S uv run --quiet --script
# /// script
# requires-python = ">=3.10"
# dependencies = ["websockets", "pillow"]
# ///
"""Film the container's terminal: ttyd serves a tmux session over HTTP, this
drives it with `tmux send-keys` and screenshots the real rendered terminal
from Chrome. Browser-driven motion route, applied to a TUI.

Usage: film.py <segment-name> <ttyd-url> <container>
Segments: install | verify
"""

import asyncio
import base64
import json
import subprocess
import sys
import time
from pathlib import Path

import websockets

HERE = Path(__file__).resolve().parent
FPS = 2.0                      # screenshot cadence
DEBUG_PORT = 7222


async def cdp(ws, mid, method, params=None):
    await ws.send(json.dumps({"id": mid, "method": method, "params": params or {}}))
    while True:
        msg = json.loads(await ws.recv())
        if msg.get("id") == mid:
            return msg.get("result", {})


def wait_for_shell(container, timeout=600):
    """Never type into a running program's stdin: keys sent while an agent is
    still working land in *its* input and are echoed as text, not run."""
    import time as _t
    deadline = _t.time() + timeout
    while _t.time() < deadline:
        out = subprocess.run(["docker", "exec", container, "tmux",
                              "display-message", "-p", "-t", "demo",
                              "#{pane_current_command}"],
                             capture_output=True, text=True)
        if out.stdout.strip() in ("bash", "sh", "zsh"):
            return True
        _t.sleep(2)
    raise SystemExit("shell never came back; refusing to type into a running program")


def tmux(container, keys, enter=True):
    cmd = ["docker", "exec", container, "tmux", "send-keys", "-t", "demo", keys]
    if enter:
        cmd.append("Enter")
    subprocess.run(cmd, check=True, capture_output=True)


async def shoot(ws, outdir, stop, counter):
    """Screenshot the page at FPS until stop is set."""
    mid = 1000
    while not stop.is_set():
        mid += 1
        try:
            res = await asyncio.wait_for(
                cdp(ws, mid, "Page.captureScreenshot", {"format": "png"}), timeout=3)
            data = res.get("data")
            if data:
                (outdir / f"f{counter[0]:05d}.png").write_bytes(base64.b64decode(data))
                counter[0] += 1
        except asyncio.TimeoutError:
            pass            # a capture racing a redraw: drop the frame, keep filming
        await asyncio.sleep(1 / FPS)


async def main(segment, url, container):
    outdir = HERE / "frames" / segment
    outdir.mkdir(parents=True, exist_ok=True)
    for old in outdir.glob("*.png"):
        old.unlink()

    chrome = subprocess.Popen([
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
        f"--remote-debugging-port={DEBUG_PORT}", "--headless=new",
        "--user-data-dir=" + str(HERE / "chrome-profile"),
        "--window-size=1280,760", "--hide-scrollbars",
        "--force-device-scale-factor=2",
        "--use-gl=angle", "--use-angle=swiftshader",
        "--enable-unsafe-swiftshader", "about:blank",
    ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        time.sleep(3)
        import urllib.request
        tabs = json.loads(urllib.request.urlopen(
            f"http://127.0.0.1:{DEBUG_PORT}/json").read())
        ws_url = [t for t in tabs if t["type"] == "page"][0]["webSocketDebuggerUrl"]

        async with websockets.connect(ws_url, max_size=40 * 1024 * 1024) as ws:
            await cdp(ws, 1, "Page.enable")
            await cdp(ws, 3, "Page.navigate", {"url": url})
            await asyncio.sleep(4)
            subprocess.run(["docker", "exec", container, "tmux",
                            "refresh-client", "-t", "demo"], capture_output=True)
            await asyncio.sleep(1)
            await preflight(ws, container)

            stop = asyncio.Event()
            counter = [0]
            task = asyncio.create_task(shoot(ws, outdir, stop, counter))

            await {"install": beats_install, "agent": beats_agent,
                   "play": beats_play, "verify": beats_verify}[segment](container)

            stop.set()
            await task
            print(f"{segment}: {counter[0]} frames -> {outdir}")
    finally:
        chrome.terminate()
        try:
            chrome.wait(timeout=5)
        except Exception:
            chrome.kill()


async def preflight(ws, container):
    wait_for_shell(container)
    """Refuse to film a blank terminal. The xterm canvas renders empty under
    headless GPU-less Chrome unless software GL is on; filming 70 blank
    frames and discovering it afterwards is the failure this guards."""
    tmux(container, "echo PREFLIGHT_OK")
    await asyncio.sleep(1.5)
    res = await cdp(ws, 900, "Page.captureScreenshot", {"format": "png"})
    png = base64.b64decode(res["data"])
    from PIL import Image
    import io
    im = Image.open(io.BytesIO(png)).convert("L")
    px = list(im.getdata())
    lit = sum(1 for v in px if v > 90) / len(px)
    if lit < 0.002:
        raise SystemExit(
            f"preflight failed: terminal renders blank ({lit:.4%} lit pixels). "
            "Check software GL flags before filming.")
    print(f"preflight ok: {lit:.2%} of pixels lit")
    tmux(container, "clear")
    await asyncio.sleep(0.8)


async def beats_install(c):
    await asyncio.sleep(2.5)
    tmux(c, "claude plugin list")          # clean machine: nothing installed
    await asyncio.sleep(6)
    tmux(c, "clear")
    await asyncio.sleep(0.6)
    tmux(c, "claude plugin marketplace add prime-radiant-inc/proving-it-works")
    await asyncio.sleep(13)
    tmux(c, "claude plugin install proving-it-works")
    await asyncio.sleep(11)
    tmux(c, "claude plugin list")
    await asyncio.sleep(7)


async def beats_agent(c):
    await asyncio.sleep(2)
    tmux(c, "clear")
    await asyncio.sleep(0.8)
    tmux(c, 'claude --permission-mode bypassPermissions -p "Use the '
            'proving-it-works-with-a-movie skill. Make a ~12s NARRATED movie '
            'proving /work/app/index.html counts 0 to 1 to 2 when clicked. '
            'Motion route: record continuous frames from headless chromium '
            'with the cursor overlay drawn. No API key here, so narrate with '
            'the local engine. Subtitles required: make-subtitles then '
            'burn-subtitles. Verify with check-movie. Save to '
            '/work/out/counter.mp4."')
    await asyncio.sleep(32)


async def beats_play(c):
    await asyncio.sleep(2)
    tmux(c, "clear")
    await asyncio.sleep(0.8)
    tmux(c, "mpv --vo=tct --really-quiet --no-audio out/counter.mp4")
    await asyncio.sleep(16)


async def beats_verify(c):
    await asyncio.sleep(2.5)
    tmux(c, "clear")
    await asyncio.sleep(0.6)
    tmux(c, "ls -la out/counter.mp4")
    await asyncio.sleep(4)
    tmux(c, "SKILL=$(dirname $(find ~/.claude/plugins -path "
            "'*proving-it-works-with-a-movie*' -name SKILL.md | head -1))")
    await asyncio.sleep(2.5)
    tmux(c, "$SKILL/scripts/check-movie out/counter.mp4 --no-expect-audio")
    await asyncio.sleep(26)


if __name__ == "__main__":
    asyncio.run(main(sys.argv[1], sys.argv[2], sys.argv[3]))
