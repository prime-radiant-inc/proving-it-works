# Narrating

Narration is where the most embarrassing silent failures live: the movie
looks perfect and says the wrong words.

## Engines

`movie build` narrates every scene that has `narration`, with the scene
file's `engine`:

| Engine | What it is | Watch for |
|---|---|---|
| `auto` (default) | `openai` when `OPENAI_API_KEY` (or `llm keys get openai`) yields a key, else `piper` | |
| `openai` | OpenAI's speech endpoint | Reads exactly what it is sent. The safe default. |
| `openai-chat` | A chat model with audio output | Best prosody, but it ad-libs ("Sure, here it is:"). Gated: its own transcript must match the script word for word, or the clip is rejected and retried once. That proves what the model says it said, not what is in the audio. |
| `piper` | A local neural voice, free and offline | **Mispronounces** unusual names rather than dropping them. |

Local TTS engines other than these can drop out-of-vocabulary words
silently with a zero exit code ("every eval on the shelf" became "every on
the shelf"). That is why `build` offers only these three.

## The local voice

Install Piper once, and its default voice once:

```bash
uv tool install piper-tts
uvx --from piper-tts python -m piper.download_voices --data-dir ~/.cache/piper-voices en_US-lessac-medium
```

`build` looks for voices in `PIPER_VOICE_DIR`, default
`~/.cache/piper-voices`, and names the exact command if something is
missing. It never downloads anything itself.

## Listen to it

Nothing in `build` hears the audio. Before you commit to a voice, listen to
a clip of your actual sentences, including product names and jargon. A voice
that mangles the one word your movie is about is worse than no narration.
After the build, play the movie with sound on.

## Clips are cached

`build` keeps each accepted clip in `movie.build/narration/`, named by its
text, engine, voice, and model, and reuses it while all four are unchanged.
It prints each clip's path. To redo a clip that sounds wrong, delete that
file and build again.

## Measured, never guessed

`build` measures every clip and holds each scene for max(narration,
visuals), so nothing is paced against a guess. When you record motion to
match narration, build once, read each scene's length from the build
output, and pace the recording to it. Word-count estimates (~2.5 words/sec)
are for planning the script only.

## Pronunciation of product names

If a voice mangles one term, try another voice first. Respelling the term
phonetically in `narration` works, but the subtitles will show the
respelling too.
