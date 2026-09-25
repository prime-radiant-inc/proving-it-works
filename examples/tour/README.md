# The tour

A movie of Claude using the skill to film three kinds of subject, a
terminal, a web app, and a desktop calculator, on macOS and on Linux. What
the movie shows is a terminal Claude types into, filmed with `movie term`.
After each `stop` it cuts to the recording those commands just made.

| File | What it is |
|---|---|
| `film.sh mac\|linux` | drives the filmed terminal on one platform |
| `Dockerfile` | the Linux machine: tmux, chromium, Xvfb, galculator, xdotool, ffmpeg |
| `app/todo`, `app/index.html` | the subjects: a tiny CLI and a todo web app |
| `scenes.yaml` | the cut: title, macOS, Linux, end card |

```bash
docker build -t movie-tour examples/tour
examples/tour/film.sh linux
examples/tour/film.sh mac        # Terminal needs Screen Recording and Accessibility; don't type while it films Calculator
skills/proving-it-works-with-a-movie/bin/movie build examples/tour/scenes.yaml examples/tour/out/tour.mp4
```

Everything filmed lands in `out/`, which is not committed. Move `out/` away
before filming a platform again.
