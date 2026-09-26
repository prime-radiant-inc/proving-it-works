# Distributing movie: download on first run

Paths below are relative to the repo root; `BIN` is
`skills/proving-it-works-with-a-movie/bin`.

## Problem

Plugin installs clone this public repo, and `BIN` carries five committed
release binaries (about 10 MB each) that `BIN/movie` picks between. Every
Go change so far has been followed by a rebuild commit. After 14 of them, a
fresh clone is 80 MB, 68 MB of it old binaries, and each rebuild adds about
5 MB that no clone ever sheds.

Separately, the plugin version has stayed 0.1.0 through every change, and
Claude Code caches installed plugins by version, so existing users may
never see an update.

## Decision

The repo carries the Go source, the launchers, and two small committed
files: `BIN/VERSION` and `BIN/checksums.txt` (the SHA-256 of each
platform's binary for that version). The binaries live on the GitHub
release `vVERSION`. On first run the launcher downloads the one binary it
needs, verifies it against `checksums.txt`, caches it, and runs it.

There is one version: `everyharness.yaml`'s, which everyharness copies into
the plugin manifests, and which `script/release` also writes to
`BIN/VERSION`. Every change users should get ships as a release, skill text
included, so the version always moves when the plugin changes.

Rejected:

- Keep committing binaries, rebuilt only at release: works offline with no
  install step, but the clone still grows about 5 MB a release, forever.
- Port to TypeScript or Python: a rewrite of about 7,000 lines of Go
  (a font-rendering terminal renderer, a CDP client, PNG work) to move a
  distribution problem, not solve it. ffmpeg is needed either way.
- Build from source on first run: needs Go on every user's machine. It is
  kept as a fallback, below.

## What the checksum protects against

`checksums.txt` is committed, so it is reviewable and pinned with the rest
of the plugin. It catches a truncated or corrupted download, a proxy or
mirror serving something else, and a release asset replaced after the
fact. It does not protect against someone who can push to this repo, who
could change both.

## The launchers

`BIN/movie` (sh: macOS, Linux, and Windows through Git Bash) and
`BIN/movie.ps1` (Windows PowerShell) do the same thing:

1. Work out the platform. Windows is always `windows-amd64`, as today (it
   runs under emulation on ARM); elsewhere `darwin|linux` and
   `amd64|arm64`. Read `BIN/VERSION` and the platform's line of
   `BIN/checksums.txt`, stripping any carriage return.
2. The binary's cache path is `ROOT/proving-it-works/SHA256/movie-OS-ARCH[.exe]`,
   keyed by the expected hash, so only a verified file can ever be at a
   path the launcher trusts. ROOT is `%LOCALAPPDATA%` on Windows (from both
   launchers, so Git Bash and PowerShell share one cache), else
   `$XDG_CACHE_HOME`, else `~/.cache`. If ROOT is not writable, it is the
   system temp directory, and the launcher says so on stderr.
3. If the file is there, run it with the arguments untouched (on Git Bash,
   with the MSYS path-conversion guards the current launcher sets).
4. If not, download
   `https://github.com/prime-radiant-inc/proving-it-works/releases/download/vVERSION/movie-OS-ARCH[.exe]`
   (`MOVIE_RELEASE_URL` overrides the base) into a uniquely named temporary
   file beside the cache path: `curl -fsSL`, else `wget -q -O` (sh), or
   `Invoke-WebRequest -UseBasicParsing` with
   `$ProgressPreference = 'SilentlyContinue'` (PowerShell). Any HTTP failure
   is a download failure (step 6), never a checksum mismatch.
5. Check the file's SHA-256 (`sha256sum`, else `shasum -a 256`;
   `Get-FileHash`). On a match, make it executable and rename it into
   place; if the rename fails because another first run got there first,
   delete the temporary file and use the one in place (it is at a
   hash-keyed path, so it is verified). On a mismatch, delete it and exit 2
   naming both hashes. After installing, delete the other hash directories,
   so old versions do not pile up.
6. If the download failed, exit 2 with the URL, the cache path to put the
   file at, and its expected SHA-256, so a person or agent can fetch it by
   other means, and with the command to build it from source when `go` is
   on PATH. The launcher never builds by itself: a source build needs
   network access to fetch modules, and (with `go.mod` at 1.26.1) possibly
   a newer toolchain, so it would not rescue the offline case, and a build
   is not the verified release.

One environment variable is for working on movie itself:
`MOVIE_FROM_SOURCE=1` (sh launcher only) builds from the checkout's source
with the release flags into `ROOT/proving-it-works/source/`, rebuilding only
when the source tree's contents change, and runs that. It is how an
unreleased checkout is tested with `--plugin-dir`.

`movie.ps1` runs under Windows' default execution policy only when invoked
as `powershell -ExecutionPolicy Bypass -File "$SKILL_DIR\bin\movie.ps1"`,
which SKILL.md's PowerShell line will say.

## Releasing

`script/release VERSION`, run from `main`:

1. Refuses unless the tree is clean, `HEAD` equals `origin/main` after a
   fetch, and `vVERSION` exists neither locally nor on `origin`.
2. Builds the five binaries with `script/build-binaries` into a temporary
   directory (already reproducible: pinned toolchain, `-trimpath`,
   `-buildvcs=false`), and writes `BIN/checksums.txt` from them.
3. Sets the version in `everyharness.yaml`, regenerates the harness files
   with everyharness, writes `BIN/VERSION`, and commits.
4. Creates `vVERSION` as a draft GitHub release with the five binaries
   (drafts are not publicly downloadable), pushes the tag and `main`, then
   publishes the draft. If the push fails, it deletes the draft and the
   tag, so nothing public names a commit off `main`.
5. Downloads each published asset and verifies it against
   `checksums.txt`.

## Checks

- The release check (`MOVIE_RELEASE_CHECK=1`, run in CI on `main`) builds
  the five binaries from source and compares their hashes with
  `BIN/checksums.txt`, so `main`'s source always matches what its release
  serves. It replaces today's byte comparison with committed binaries.
- Launcher tests serve a real binary of the test's platform and a
  `checksums.txt` from a local HTTP server through `MOVIE_RELEASE_URL`,
  with a temporary cache root:
  - first run downloads, verifies, caches, and runs, passing arguments and
    exit codes through; the second run does not download;
  - a checksum mismatch exits 2, names both hashes, and leaves nothing in
    the cache;
  - a 404 exits 2 with the URL, path, and hash;
  - an unwritable cache root falls back to the temp directory;
  - two first runs at once both succeed (on Windows too);
  - older hash directories are removed after an install.
  The sh launcher's tests run on Linux, macOS, and Windows (Git Bash) in
  CI; the PowerShell launcher's on Windows.

## Migration

One commit, released as the next version:

- removes the five binaries from `BIN`, and adds `BIN/movie.ps1`,
  `BIN/VERSION`, `BIN/checksums.txt`, and `script/release`;
- rewrites `BIN/movie`;
- `.gitattributes`: `text eol=lf` for `BIN/VERSION`, `BIN/checksums.txt`,
  and `BIN/movie.ps1`; drops the `BIN/movie-*` binary rule;
- `.gitignore`: `BIN/movie-*`, so a stray build is never committed;
- `script/build-binaries`: OUTDIR becomes required, and its header stops
  saying to commit the result;
- `cmd/movie/release_test.go`: the freshness test compares hashes, and the
  argument pass-through test runs the new launcher against a local server;
- `examples/tour/film.sh`: cross-builds its Linux binary with
  `script/build-binaries` instead of mounting a committed one;
- SKILL.md's PowerShell line, and README.md's "prebuilt" install text.

Existing installs keep their committed binaries until they update. History
keeps the old binaries; shrinking clones below 80 MB would mean rewriting
published history, which this does not do.

## What this gives up

A sandbox that can neither reach GitHub on first run nor write outside its
workspace cannot run movie until someone fetches the binary once, as the
error message describes. Committed binaries worked there with no step at
all.
