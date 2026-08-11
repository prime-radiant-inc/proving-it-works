# Composited stills

The cheap route, and the right one whenever the *sequence of states* is the
claim and motion is decoration. Real screenshots of the running product,
captioned, held long enough to read.

Adapted from `rendering-a-demo-movie.md` in obra/superpowers PR #1931.

## 1. Capture real scene frames

Fix the viewport first so every frame composes identically. Per beat:
navigate or drive the app into the state, screenshot to `frame-NN.png`, and
**read the PNG back** to confirm you got the state you meant. One deliberate
screenshot per beat; no fps.

The read-back is not optional. It is what catches a shot taken mid-scroll,
mid-animation, or before a fetch resolved — the defect that otherwise ships.

## 2. Composite cards in the browser

Render title, caption, and end cards as HTML and screenshot them, rather
than fighting ffmpeg `drawtext` (see assembling.md). A single param-driven
`card.html` covers all three shapes:

```html
<!doctype html><meta charset="utf-8">
<style>
  body { margin:0; width:1400px; height:960px; overflow:hidden;
         font-family:Georgia,serif; background:#faf8f4; }
  .frame { width:1400px; height:900px; display:block; }
  .bar { width:1400px; height:60px; background:#2a2722; color:#faf8f4;
         display:flex; align-items:center; justify-content:center; font-size:26px; }
  .title { height:960px; display:flex; flex-direction:column;
           align-items:center; justify-content:center; gap:24px; }
</style>
<body><script>
  const q = new URLSearchParams(location.search);
  if (q.get("mode") === "title")
    document.body.innerHTML = '<div class="title"><h1>App</h1><p>tagline</p></div>';
  else
    document.body.innerHTML = '<img class="frame" src="' + q.get("img") +
      '"><div class="bar">' + q.get("cap") + '</div>';
</script></body>
```

Screenshot each composed card to `card-NN.png`, zero-padded so a lexical
glob orders them: `card-00` title, `card-01..NN` scenes, `card-99` end.

## 3. Hold each card for its narration

If the movie is narrated, each card's duration is its narration clip's
measured length (plus a short beat), not a fixed interval. This is what
keeps a stills movie in sync by construction — the picture advances exactly
when the sentence about it ends.

Unnarrated, `-framerate 1/3` (3s per card) is a reasonable default; anything
faster than ~2.5s is unreadable.

## 4. Gate it

Run `"$SKILL_DIR/scripts/check-movie"` (see SKILL.md for the path), open the
contact sheet, and look. A stills movie earns a
frozen-tail warning when its final card outlasts its last narration by a
lot — that usually means the closing card is doing too much work, or the
last scene should have been two.
