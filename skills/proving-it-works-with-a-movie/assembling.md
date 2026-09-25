# Assembling

`movie build scenes.yaml movie.mp4` turns clips, stills, cards, and
narration into one checked file. This page is the scene file, and the
ffmpeg traps worth knowing when you make the pieces yourself.

## The scene file

```yaml
size: 1920x1080              # default; width and height must be even
fps: 30                      # default
engine: auto                 # auto | openai | openai-chat | piper (narrating.md)
voice: nova                  # default: nova for openai*, en_US-lessac-medium for piper

scenes:
  - id: title                # [a-z0-9][a-z0-9-]*, unique
    card: proving-it-works   # a title card, drawn for you
    subtitle: installed from a public marketplace
    duration: 4              # card and image: seconds to hold, default 3

  - id: install
    frames: takes/install/take-1/   # a directory of PNGs, in name order
    rate: 10                        # frames per second; default: the take.json rate if the directory has one, else fps
    narration: >-
      This is a container with nothing of ours in it.
    narration_at: end               # optional: finish the narration as the scene ends

  - id: sheet
    image: work/out/contact-sheet.png
    narration: So we do.

  - id: movie
    movie: work/out/counter.mp4     # played as itself, with its own audio
```

A scene is whichever one of `card`, `image`, `frames`, or `movie` it has.
Paths are relative to the scene file. `build` validates the whole file
before doing any work and lists every problem at once.

## The segment rule

Each scene lasts **max(narration, visuals)**. Short video freezes its last
frame; short audio pads with silence. Narration starts with its scene; with
`narration_at: end` it is delayed so it ends as the scene ends, and its
subtitles move with it (use it where the scene's payoff comes last, as in a
terminal take). A `movie` scene lasts as long as the
movie and keeps its own sound; it takes no narration.

**A long freeze-frame tail is a smell, not a fix.** If a scene's narration
runs 20 seconds past its visuals, the scene is wrong: give the camera
something to do, or cut the words.

## Cards

`card` scenes are drawn by `build` itself: title and subtitle centered on a
dark background, shrunk until the longest line fits. The card font covers
Latin, Greek, and Cyrillic; `build` refuses card text it cannot draw rather
than drawing boxes. For anything else, make an image and use an `image`
scene.

## What build writes

- `movie.mp4`, the movie.
- `movie.srt`, the subtitles, beside it: burned into the picture where
  ffmpeg has libass, and the transcript the checker reads. Without libass,
  `build` embeds a soft track instead and warns that the subtitles are not
  in the picture: anything that autoplays without subtitle UI (Slack, PR
  previews) will show none.
- `movie-check/contact-sheet.png`, the sheet to look at.
- `movie.build/`, scratch: segments and cached narration. Safe to delete.

`build` runs the gate expecting audio and subtitles exactly when something
is narrated. Run alone on a movie made elsewhere, `movie check` expects
both; pass `--no-expect-audio` for a movie meant to be silent and
`--no-expect-subtitles` for one meant to have no subtitles.

## `-nostdin` on every ffmpeg call inside a loop

When you make the pieces yourself, remember ffmpeg reads stdin by default
and will eat a loop's input:

```bash
while IFS= read -r scene; do
  ffmpeg -nostdin ...          # without this, ffmpeg swallows the rest of the list
done < scenes.txt
```

Symptom when you forget: scenes silently skipped, and an error naming a
*truncated* identifier (`val-landing` for `eval-landing`). It reads like a
corrupt input file.

## Keep the pipeline out of scratch

The scene file, narration text, and whatever script records the frames
belong in the repo. Scratch directories get cleaned; a movie you cannot
rebuild is a movie you cannot fix. Ask before committing large media; the
pipeline is small and always worth committing.
