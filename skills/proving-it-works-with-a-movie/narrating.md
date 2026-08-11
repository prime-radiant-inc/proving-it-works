# Narrating

Narration is where the most embarrassing silent failures live: the movie
looks perfect and says the wrong words.

## Choosing a voice

Listen to a sample of your actual sentences — including product names and
jargon — before you render anything with it. A voice that mangles the one
word your movie is about is worse than no narration.

| Engine | Watch for |
|---|---|
| OS built-ins (`say`) | Free and instant; reliably sounds robotic. Fine for a scratch timing pass, not for delivery. |
| Cloud TTS endpoints (e.g. `/v1/audio/speech`) | Deterministic: reads exactly what you send. The safe default. |
| Chat models with audio output | Best prosody, but they are *chat models*: they ad-lib preambles ("Sure, here it is:"). Usable only with a verbatim gate. |
| Local neural TTS (Kokoro etc.) | Free, offline, decent — and drops out-of-vocabulary words **silently**, with a zero exit code. "Every eval on the shelf" became "every on the shelf" with no error at all. |

## The verbatim gate — required

Never trust the generator's own account of what it produced. Verify the
audio that is actually in the file:

```bash
# transcribe the RENDERED audio, then diff against the source script
ffmpeg -nostdin -v error -i movie.mp4 -map 0:a -ac 1 -ar 16000 narration.wav
# send narration.wav to a transcription API, then compare word sequences
```

A word-sequence diff (lowercase, strip punctuation) catches dropped jargon,
ad-libbed preambles, and whole missing sentences. If the engine returns its
own transcript, diff that too — it is a cheap early signal — but the
rendered audio is the artifact that ships, so it is the one that counts.

When drift is found: regenerate that block and re-verify. Retrying once
clears chat-model preambles almost every time.

## Measure durations; never guess them

The single most common defect in a narrated movie is motion paced against
narration that nobody timed. Write the script, render the audio, `ffprobe`
each clip, *then* build video to those measured lengths.

```bash
ffprobe -v error -show_entries format=duration -of csv=p=0 narration/scene-03.wav
```

Word-count estimates (~2.5 words/sec) are for planning the script only.
Real delivery runs long and varies per block.

## Pronunciation of product names

Check the sample for your own jargon before committing to a voice. If a good
voice mangles one term, spell it phonetically **in the TTS input only**
("S M evals"), never in the script file a human reads. Keep that
substitution in the narrate step so the source text stays clean.
