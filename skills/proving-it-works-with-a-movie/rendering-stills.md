# Stills

The cheap route, and the right one whenever the *sequence of states* is the
claim and motion is decoration. Real screenshots of the running product,
held long enough to read, with subtitles carrying the words.

Adapted from `rendering-a-demo-movie.md` in obra/superpowers PR #1931.

## 1. Capture real scene frames

Fix the viewport first so every frame composes identically. Per beat:
navigate or drive the app into the state, screenshot to `frame-NN.png`, and
**read the PNG back** to confirm you got the state you meant. One deliberate
screenshot per beat; no fps.

The read-back is not optional. It is what catches a shot taken mid-scroll,
mid-animation, or before a fetch resolved — the defect that otherwise ships.

## 2. Each screenshot is an `image` scene

Do not composite caption bars onto the stills. Subtitles carry the words
now (assembling.md), so a caption strip burned into each frame duplicates
them, competes with them, and has to be re-rendered every time you reword a
sentence. The screenshot is the evidence; leave it alone.

Give each screenshot its own `image` scene, in order, with its own
`narration` (assembling.md). `movie build` holds it for max(narration,
`duration`) — the picture advances exactly when the sentence about it ends.

## 3. Title and end cards need no compositing

A title and an end card are still worth having, but they are `card` scenes,
not images: `build` draws them for you, centered and wrapped (assembling.md),
so there is nothing to render as HTML and screenshot.

## 4. Gate it

`movie build` runs the gate as part of assembling the movie. Open the
contact sheet it writes and look. A stills movie earns a frozen-tail warning
when its final card outlasts its last narration by a lot — that usually
means the closing card is doing too much work, or the last scene should have
been two.
