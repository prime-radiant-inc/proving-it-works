package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const binDir = "skills/proving-it-works-with-a-movie/bin"

var releaseNames = []string{
	"movie-darwin-arm64", "movie-darwin-amd64", "movie-linux-amd64",
	"movie-linux-arm64", "movie-windows-amd64.exe", "movie-windows-arm64.exe",
}

// repoRoot is the repository root; tests run in cmd/movie.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

func copyFile(t *testing.T, from, to string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, mode); err != nil {
		t.Fatal(err)
	}
}

// readChecksums parses a checksums.txt into asset name -> SHA-256.
func readChecksums(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("%s: malformed line %q", path, line)
		}
		sums[fields[1]] = fields[0]
	}
	return sums
}

// The release main names must be what its source builds: checksums.txt
// holds the SHA-256 of each binary script/build-binaries makes from this
// source, and VERSION is everyharness.yaml's version. CI runs it on main.
func TestReleaseChecksumsMatchSource(t *testing.T) {
	if os.Getenv("MOVIE_RELEASE_CHECK") != "1" {
		t.Skip("set MOVIE_RELEASE_CHECK=1 to compare checksums.txt with a fresh build")
	}
	root := repoRoot(t)
	out := t.TempDir()
	if got := buildBinaries(t, out); got.code != 0 {
		t.Fatalf("build-binaries: exit %d\n%s", got.code, got.stderr)
	}
	sums := readChecksums(t, filepath.Join(root, binDir, "checksums.txt"))
	if len(sums) != len(releaseNames) {
		t.Errorf("checksums.txt lists %d assets, want %d", len(sums), len(releaseNames))
	}
	for _, name := range releaseNames {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := sha256Hex(data); got != sums[name] {
			t.Errorf("%s builds to %s, but checksums.txt says %q: release this source with script/release", name, got, sums[name])
		}
	}
	version, err := os.ReadFile(filepath.Join(root, binDir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(root, "everyharness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "\nversion: "+strings.TrimSpace(string(version))+"\n") {
		t.Errorf("VERSION says %q, but everyharness.yaml's version differs", strings.TrimSpace(string(version)))
	}
}

// buildBinaries runs script/build-binaries with args from the repo root.
func buildBinaries(t *testing.T, args ...string) result {
	t.Helper()
	root := repoRoot(t)
	cmd := exec.Command("bash", append([]string{filepath.Join(root, "script", "build-binaries")}, args...)...)
	cmd.Dir = root
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// build-binaries needs somewhere to put the binaries: they never go in the
// repo, and an unknown target is refused rather than guessed at.
func TestBuildBinariesRefusesAMissingOutdirOrUnknownTarget(t *testing.T) {
	if got := buildBinaries(t); got.code != 2 || !strings.Contains(got.stderr, "usage: script/build-binaries OUTDIR") {
		t.Errorf("no OUTDIR: exit %d\n%s", got.code, got.stderr)
	}
	got := buildBinaries(t, t.TempDir(), "plan9/amd64")
	if got.code != 2 || !strings.Contains(got.stderr, "unknown target plan9/amd64") || !strings.Contains(got.stderr, "windows/arm64") {
		t.Errorf("unknown target: exit %d\n%s", got.code, got.stderr)
	}
}

// Named targets build only those; Windows on ARM is one of them.
func TestBuildBinariesBuildsOnlyTheNamedTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles movie")
	}
	out := t.TempDir()
	if got := buildBinaries(t, out, "windows/arm64"); got.code != 0 {
		t.Fatalf("exit %d\n%s", got.code, got.stderr)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "movie-windows-arm64.exe" {
		t.Fatalf("built %v, want only movie-windows-arm64.exe", entries)
	}
}

// git runs git in dir as a test identity, failing the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), gitIdentity...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

var gitIdentity = []string{
	"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
	"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
}

// releaseClone is a clone of this repo on main, with the working tree's
// release scripts committed and a bare origin it has pushed to, so
// script/release can run there without touching this repo or GitHub.
func releaseClone(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	root := repoRoot(t)
	base := t.TempDir()
	dir, origin := filepath.Join(base, "work"), filepath.Join(base, "origin.git")
	// Local clones hardlink this repo's objects, so only the commit below is
	// ever packed; pushing the whole history into an empty bare repo would
	// repack every old binary in it.
	git(t, base, "clone", "-q", "--bare", root, origin)
	git(t, base, "clone", "-q", origin, dir)
	git(t, dir, "checkout", "-q", "-B", "main")
	for _, s := range []string{"script/release", "script/build-binaries"} {
		copyFile(t, filepath.Join(root, s), filepath.Join(dir, s), 0o755)
	}
	git(t, dir, "add", "script")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "the scripts under test")
	git(t, dir, "push", "-q", "--force", "origin", "main")
	return dir
}

// runRelease runs script/release in dir with env added.
func runRelease(t *testing.T, dir string, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(dir, "script", "release")}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), gitIdentity...), env...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// script/release refuses to start anywhere a release could go wrong: off
// main, with uncommitted changes, behind origin, or on a version already
// tagged. Each refusal happens before anything is built or committed.
func TestReleaseRefusesToStartFromTheWrongPlace(t *testing.T) {
	cases := []struct {
		name    string
		version string
		setup   func(t *testing.T, dir string)
		want    string
	}{
		{"not a version", "9.9", func(*testing.T, string) {}, "usage: script/release VERSION"},
		{"off main", "9.9.9", func(t *testing.T, dir string) { git(t, dir, "checkout", "-q", "-b", "topic") }, "run it on main"},
		{"uncommitted changes", "9.9.9", func(t *testing.T, dir string) { writeText(t, filepath.Join(dir, "stray.txt"), "x") }, "uncommitted changes"},
		{"behind origin", "9.9.9", func(t *testing.T, dir string) {
			git(t, dir, "commit", "-q", "--allow-empty", "-m", "someone else's")
			git(t, dir, "push", "-q", "origin", "main")
			git(t, dir, "reset", "-q", "--hard", "HEAD~1")
		}, "pull first"},
		{"tagged here", "9.9.9", func(t *testing.T, dir string) { git(t, dir, "tag", "v9.9.9") }, "v9.9.9 already exists here"},
		{"tagged on origin", "9.9.9", func(t *testing.T, dir string) {
			git(t, dir, "tag", "v9.9.9")
			git(t, dir, "push", "-q", "origin", "v9.9.9")
			git(t, dir, "tag", "-d", "v9.9.9")
		}, "v9.9.9 already exists on origin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := releaseClone(t)
			c.setup(t, dir)
			before := git(t, dir, "rev-parse", "HEAD")
			got := runRelease(t, dir, nil, c.version)
			if got.code != 2 || !strings.Contains(got.stderr, c.want) {
				t.Errorf("exit %d, want 2 saying %q:\n%s", got.code, c.want, got.stderr)
			}
			if after := git(t, dir, "rev-parse", "HEAD"); after != before {
				t.Errorf("committed despite refusing")
			}
		})
	}
}

// A dry run makes the release commit: the version everywhere, VERSION, and
// the six binaries' checksums; and tags, pushes, and publishes nothing.
func TestReleaseDryRunCommitsTheVersionAndChecksums(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the six release binaries")
	}
	if _, err := exec.LookPath("everyharness"); err != nil && os.Getenv("EVERYHARNESS") == "" {
		t.Skip("needs everyharness on PATH or EVERYHARNESS=/path/to/everyharness/dist/cli.js")
	}
	dir := releaseClone(t)
	pushed := git(t, dir, "rev-parse", "origin/main")
	got := runRelease(t, dir, []string{"RELEASE_DRY_RUN=1"}, "9.9.9")
	if got.code != 0 {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, binDir, "VERSION")); string(data) != "9.9.9\n" {
		t.Errorf("VERSION holds %q", data)
	}
	sums := readChecksums(t, filepath.Join(dir, binDir, "checksums.txt"))
	for _, name := range releaseNames {
		if len(sums[name]) != 64 {
			t.Errorf("checksums.txt has no hash for %s: %v", name, sums)
		}
	}
	for file, want := range map[string]string{"everyharness.yaml": "\nversion: 9.9.9\n", ".claude-plugin/plugin.json": `"version": "9.9.9"`} {
		if data, _ := os.ReadFile(filepath.Join(dir, file)); !strings.Contains(string(data), want) {
			t.Errorf("%s lacks %q", file, want)
		}
	}
	if subject := git(t, dir, "log", "-1", "--format=%s"); subject != "Release 9.9.9" {
		t.Errorf("last commit is %q", subject)
	}
	if status := git(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("left uncommitted:\n%s", status)
	}
	if tags := git(t, dir, "tag", "-l", "v9.9.9"); tags != "" {
		t.Error("a dry run tagged")
	}
	if now := git(t, dir, "ls-remote", "origin", "refs/heads/main"); !strings.HasPrefix(now, pushed) {
		t.Error("a dry run pushed")
	}
}
