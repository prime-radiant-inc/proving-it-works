# Distributing movie: download on first run

## Problem

Plugin installs clone this public repo, and `skills/proving-it-works-with-a-movie/bin/`
carries five committed release binaries (about 10 MB each) that `bin/movie`
picks between. Every Go change so far has been followed by a rebuild
commit. After 14 of them, a fresh clone is 80 MB, 68 MB of it old binaries,
and each rebuild adds about 5 MB that no clone ever sheds.

## Decision

The repo carries the Go source, the launchers, and two small committed
files: `bin/VERSION` and `bin/checksums.txt` (the SHA-256 of each platform's
binary for that version). The binaries live on the GitHub release
`vVERSION`. On first run the launcher downloads the one binary it needs,
verifies it against `checksums.txt`, caches it, and runs it.

Rejected:

- Keep committing binaries, rebuilt only at release: works offline with no
  install step, but the clone still grows about 5 MB a release, forever.
- Port to TypeScript or Python: a rewrite of about 7,000 lines of Go
  (a font-rendering terminal renderer, a CDP client, PNG work) to move a
  distribution problem, not solve it. ffmpeg is needed either way.
- Build from source on first run: needs Go on every user's machine. It is
  kept as a fallback, below.

## The launchers

`bin/movie` (sh, used on macOS, Linux, and Windows through Git Bash) and
`bin/movie.ps1` (Windows PowerShell) do the same thing:

1. Work out the platform (`darwin|linux|windows` and `amd64|arm64`), read
   `bin/VERSION`, and look for the binary at
   `CACHE/proving-it-works/VERSION/movie-OS-ARCH[.exe]`, where CACHE is
   `$XDG_CACHE_HOME`, else `~/.cache` (sh), or `%LOCALAPPDATA%` (PowerShell).
   The cache lives outside the plugin directory, so a plugin update does not
   lose it and a read-only plugin directory still works.
2. If it is there, run it with the arguments untouched (on Git Bash, with
   the MSYS path-conversion guards the current launcher sets).
3. If not, download
   `https://github.com/prime-radiant-inc/proving-it-works/releases/download/vVERSION/movie-OS-ARCH[.exe]`
   with curl, else wget (sh), or `Invoke-WebRequest` (PowerShell), into a
   temporary file in the cache directory; check its SHA-256 (`sha256sum`,
   else `shasum -a 256`; `Get-FileHash`) against the line for that file in
   `checksums.txt`; on a match, make it executable and rename it into
   place, which is atomic, so parallel first runs cannot run a half-written
   file. On a mismatch, delete it and exit 2 naming both hashes. Progress
   goes to stderr, one line.
4. If the download fails and `go` is on PATH, build from the committed
   source (`go build -trimpath ./cmd/movie`, with the repo root found from
   the launcher's own path) into the same cache path, and say so on stderr.
   A source build is not checked against `checksums.txt`: it is not the
   release build, and it is what the user asked for by having Go.
5. Otherwise exit 2 with the URL, the cache path to put the file at, and
   its expected SHA-256, so a person or agent can fetch it by other means.

Two environment variables, both documented in the launcher's header:

- `MOVIE_RELEASE_URL` overrides the download base (tests, mirrors).
- `MOVIE_FROM_SOURCE=1` always builds from source into a separate cache
  path (`CACHE/proving-it-works/source/`, rebuilt every run; Go's own build
  cache keeps that fast). For working on movie itself and for testing an
  unreleased checkout with `--plugin-dir`.

`bin/movie --binary-for OS-ARCH` fetches (as above) and prints the path of
that platform's binary instead of running it, for mounting a Linux binary
into a container as `examples/tour/film.sh` does.

## Releasing

`script/release VERSION`:

1. Refuses a dirty tree, or a VERSION that already has a tag.
2. Builds the five binaries with `script/build-binaries` into a temporary
   directory (already reproducible: pinned toolchain, `-trimpath`,
   `-buildvcs=false`), and writes `bin/checksums.txt` from them.
3. Writes `bin/VERSION`, sets the plugin version in `.claude-plugin/marketplace.json`
   and the other manifests that carry one, and commits.
4. Tags `vVERSION`, pushes the tag, and runs `gh release create vVERSION`
   with the five binaries, before pushing `main`, so no clone of `main` ever
   names a version whose assets are missing.

Go changes merge to `main` with a release, as they merge today with a
rebuild commit: the source on `main` always matches its released binaries.

## Checks

- The release check (`MOVIE_RELEASE_CHECK=1`, run in CI on `main`) builds
  the five binaries from source and compares their hashes with
  `bin/checksums.txt`, replacing today's byte comparison with committed
  binaries.
- Launcher tests serve a real binary of the test's platform and a
  `checksums.txt` from a local HTTP server through `MOVIE_RELEASE_URL`, with
  a temporary cache:
  - first run downloads, verifies, caches, and runs; the second run does
    not download;
  - a checksum mismatch exits 2, names both hashes, and leaves nothing in
    the cache;
  - a failed download with Go present builds from source;
  - with no download and no Go, the message carries the URL, path, and hash;
  - two first runs at once both succeed.
  The sh launcher's tests run on Linux, macOS, and Windows (Git Bash) in CI;
  the PowerShell launcher's on Windows.

## Migration

One commit removes the five binaries from the tree, adds the launchers,
`VERSION`, `checksums.txt`, and `script/release`, updates `.gitattributes`,
`examples/tour/film.sh`, and the docs that name the binaries (SKILL.md's
PowerShell line), and is released as the next version. Existing installs
keep their committed binaries until they update. History keeps the old
binaries; shrinking clones below 80 MB would mean rewriting published
history, which this does not do.

## Risks

- **No network on first run** (some sandboxes, such as Codex modes with
  the network off): covered by the Go fallback and, failing that, a message
  that says exactly what to fetch and where to put it.
- **GitHub is down or rate-limits** anonymous downloads: same path.
- **A release whose assets fail to upload**: `script/release` pushes `main`
  last, so `main` never names it.
