# Assembling

Turning clips, stills, and narration into one file — and the ffmpeg traps
that cost the most time.

## The segment rule

Per scene, the segment lasts **max(narration, visuals)**. Whichever is
shorter gets padded:

- video short → freeze the last frame (`tpad=stop_mode=clone`)
- audio short → pad with silence (`apad`)

```bash
ffmpeg -nostdin -y -v error -i clip.mp4 -i narration.wav \
  -filter_complex "[0:v]tpad=stop_mode=clone:stop_duration=${PAD}[v];[1:a]apad[a]" \
  -map "[v]" -map "[a]" -t "$DUR" -r 30 -pix_fmt yuv420p \
  -c:v libx264 -preset medium -c:a aac -ar 44100 -ac 2 segment.mp4
```

Then concat the segments (`-f concat -safe 0 -c copy`). Uniform codec
parameters across segments are what make the stream-copy concat valid.

**A long freeze-frame tail is a smell, not a fix.** If a scene's narration
runs 20 seconds past its visuals, the scene is wrong: give the camera
something to do, or cut the words.

## `-nostdin` on every ffmpeg call inside a loop

ffmpeg reads stdin by default and will eat the loop's input.

```bash
while IFS= read -r scene; do
  ffmpeg -nostdin ...          # without this, ffmpeg swallows the rest of the list
done < scenes.txt
```

Symptom when you forget: scenes silently skipped, and an error naming a
*truncated* identifier (`val-landing` for `eval-landing`) because ffmpeg
consumed part of the next line. It reads like a corrupt input file.

## Title and caption cards: render HTML, screenshot it

Do not fight `drawtext`. It is the fragile part of ffmpeg — under macOS
sandbox `textfile=` fails outright ("Either text, a valid file, a timecode
or text source must be provided") even with absolute paths. Write the card
as HTML, screenshot it in the browser you already have open, and treat it as
an image. You get real fonts, CSS layout, and markup accents for free.

Name cards so a lexical glob orders them: `card-00` (title), `card-01..NN`
(scenes), `card-99` (end).

```bash
ffmpeg -nostdin -y -v error -framerate 1/3 -pattern_type glob -i 'card-*.png' \
  -r 30 -pix_fmt yuv420p out.mp4      # 1/3 = each card holds 3s
```

## Verify the encode, then verify the content

```bash
ffprobe -v error -show_entries format=duration,size \
  -show_entries stream=codec_name,width,height -of default=noprint_wrappers=1 out.mp4
```

`ffprobe` proves the container is real. It says nothing about whether the
movie is watchable — that is `check-movie` plus your own eyes on the contact
sheet.

## Keep the pipeline out of scratch

Scene list, narration text, recorder, narrate and assemble scripts belong in
the repo. Scratch directories are cleaned by the OS between sessions; losing
the assembler mid-production means reconstructing it from prose before you
can re-cut a single scene. Ask before committing large media; the *pipeline*
is small and always worth committing.
