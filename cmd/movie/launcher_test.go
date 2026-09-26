package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testVersion is the version the launcher tests pretend is released.
const testVersion = "9.9.9"

func needSh(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs sh on PATH (Git Bash on Windows)")
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeText(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// filesUnder lists every file below dir, so a test can see what a launcher
// left behind.
func filesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// hostReleaseName is the release asset the launchers pick on this machine.
func hostReleaseName() string {
	if runtime.GOOS == "windows" {
		return "movie-windows-" + runtime.GOARCH + ".exe"
	}
	return "movie-" + runtime.GOOS + "-" + runtime.GOARCH
}

// hostBinary is the movie binary under test, which the fake release serves
// as this machine's asset.
func hostBinary(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(movieBin)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fakeRelease is a plugin bin directory holding the launchers, a VERSION,
// and a checksums.txt, with a local server standing in for GitHub releases
// and a cache root of its own.
type fakeRelease struct {
	bin, cache, url string
	want            string // the SHA-256 checksums.txt gives this machine's asset
	downloads       atomic.Int32
	requested       sync.Map // every path the server was asked for
}

// newFakeRelease serves body as this machine's asset after delay (a 404
// when body is nil), and lists want as its checksum.
func newFakeRelease(t *testing.T, want string, body []byte, delay time.Duration) *fakeRelease {
	t.Helper()
	r := &fakeRelease{bin: t.TempDir(), cache: t.TempDir(), want: want}
	for _, launcher := range []string{"movie", "movie.ps1"} {
		copyFile(t, filepath.Join(repoRoot(t), binDir, launcher), filepath.Join(r.bin, launcher), 0o755)
	}
	writeText(t, filepath.Join(r.bin, "VERSION"), testVersion+"\n")
	writeText(t, filepath.Join(r.bin, "checksums.txt"), want+"  "+hostReleaseName()+"\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.requested.Store(req.URL.Path, true)
		if body == nil || req.URL.Path != "/v"+testVersion+"/"+hostReleaseName() {
			http.NotFound(w, req)
			return
		}
		r.downloads.Add(1)
		time.Sleep(delay)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	r.url = srv.URL
	return r
}

// run runs a launcher command against the fake release and cache, with env
// added. It reports rather than stops on a failure to start, so it can run
// in goroutines.
func (r *fakeRelease) run(t *testing.T, cmd *exec.Cmd, env []string) result {
	t.Helper()
	cmd.Env = append(os.Environ(), "MOVIE_RELEASE_URL="+r.url, "XDG_CACHE_HOME="+r.cache, "LOCALAPPDATA="+r.cache)
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Errorf("running %v: %v", cmd.Args, err)
		code = -1
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// sh runs the sh launcher with args.
func (r *fakeRelease) sh(t *testing.T, env []string, args ...string) result {
	t.Helper()
	return r.run(t, exec.Command("sh", append([]string{filepath.Join(r.bin, "movie")}, args...)...), env)
}

// installed is where the launchers keep the verified asset.
func (r *fakeRelease) installed() string {
	return filepath.Join(r.cache, "proving-it-works", r.want, hostReleaseName())
}

// The launcher downloads this machine's binary once, verifies and caches it,
// and passes arguments, output, and exit codes through untouched.
func TestLauncherDownloadsVerifiesAndCachesTheBinary(t *testing.T) {
	needSh(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 0)
	got := r.sh(t, nil, "a b%c;d")
	if got.code != 2 || !strings.Contains(got.stderr, `unknown command "a b%c;d"`) {
		t.Fatalf("argument did not arrive intact (exit %d):\n%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "downloading "+hostReleaseName()+" "+testVersion) {
		t.Errorf("no download notice:\n%s", got.stderr)
	}
	if data, err := os.ReadFile(r.installed()); err != nil || sha256Hex(data) != r.want {
		t.Fatalf("not cached at %s: %v", r.installed(), err)
	}
	help := r.sh(t, nil, "help")
	if help.code != 0 || !strings.Contains(help.stdout, "Exit codes:") {
		t.Errorf("help: exit %d\n%s%s", help.code, help.stdout, help.stderr)
	}
	if n := r.downloads.Load(); n != 1 || strings.Contains(help.stderr, "downloading") {
		t.Errorf("%d downloads, want 1: the second run must use the cache\n%s", n, help.stderr)
	}
}

// A download that does not match checksums.txt is never run or kept.
func TestLauncherRefusesABinaryWithTheWrongChecksum(t *testing.T) {
	needSh(t)
	body := hostBinary(t)
	wrong := sha256Hex([]byte("not the release"))
	r := newFakeRelease(t, wrong, body, 0)
	got := r.sh(t, nil, "help")
	if got.code != 2 || !strings.Contains(got.stderr, sha256Hex(body)) || !strings.Contains(got.stderr, wrong) {
		t.Fatalf("want exit 2 naming both hashes, got %d:\n%s", got.code, got.stderr)
	}
	if strings.Contains(got.stdout, "Exit codes:") {
		t.Error("ran a binary that failed its checksum")
	}
	if left := filesUnder(t, r.cache); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

// When the download fails, the message says what to fetch, where to put
// it, what it must hash to, and how to build it instead.
func TestLauncherSaysWhatToFetchWhenTheDownloadFails(t *testing.T) {
	needSh(t)
	want := sha256Hex(hostBinary(t))
	r := newFakeRelease(t, want, nil, 0)
	got := r.sh(t, nil, "help")
	if got.code != 2 {
		t.Errorf("exit %d, want 2", got.code)
	}
	url := r.url + "/v" + testVersion + "/" + hostReleaseName()
	for _, s := range []string{"could not download " + url, "proving-it-works/" + want + "/" + hostReleaseName(), "SHA-256 must be " + want, "MOVIE_FROM_SOURCE=1"} {
		if !strings.Contains(got.stderr, s) {
			t.Errorf("message lacks %q:\n%s", s, got.stderr)
		}
	}
	if left := filesUnder(t, r.cache); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

// A cache root the user cannot write sends the cache to the temp directory.
func TestLauncherCachesInTheTempDirectoryWhenTheCacheIsReadOnly(t *testing.T) {
	needSh(t)
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory this user cannot write")
	}
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 0)
	if err := os.Chmod(r.cache, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(r.cache, 0o700) })
	tmp := t.TempDir()
	got := r.sh(t, []string{"TMPDIR=" + tmp}, "help")
	if got.code != 0 || !strings.Contains(got.stderr, "cannot write to "+r.cache) {
		t.Fatalf("exit %d:\n%s", got.code, got.stderr)
	}
	// the temp directory is shared, so the cache there is this user's alone:
	// another user could otherwise plant a binary where the launcher trusts it
	private := filepath.Join(tmp, fmt.Sprintf("proving-it-works-%d", os.Getuid()))
	if _, err := os.Stat(filepath.Join(private, r.want, hostReleaseName())); err != nil {
		t.Errorf("not cached in this user's temp directory: %v", err)
	}
	if info, err := os.Stat(private); err != nil {
		t.Errorf("no private cache: %v", err)
	} else if info.Mode().Perm() != 0o700 {
		t.Errorf("%s has mode %v, want 0700: private to this user", private, info.Mode().Perm())
	}
}

// An older release that cannot be removed does not undo a good install.
func TestLauncherRunsEvenWhenAnOldVersionCannotBeRemoved(t *testing.T) {
	needSh(t)
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory this user cannot delete from")
	}
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 0)
	locked := filepath.Join(r.cache, "proving-it-works", sha256Hex([]byte("an older release")), "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(locked, "f"), "x")
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })
	if got := r.sh(t, nil, "help"); got.code != 0 || !strings.Contains(got.stdout, "Exit codes:") {
		t.Fatalf("exit %d\n%s", got.code, got.stderr)
	}
}

// Arguments reach movie exactly: embedded quotes, and empty arguments.
// (Git Bash turns a doubled backslash into one when it starts a Windows
// program, which no script can prevent; movie.ps1 keeps it.)
func TestLauncherPassesQuotesAndEmptyArgumentsThrough(t *testing.T) {
	needSh(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 0)
	for _, arg := range []string{`say "hi" \"`, ""} {
		got := r.sh(t, nil, arg)
		if want := fmt.Sprintf("unknown command %q", arg); got.code != 2 || !strings.Contains(got.stderr, want) {
			t.Errorf("want %s, got exit %d:\n%s", want, got.code, got.stderr)
		}
	}
}

// First runs at the same moment, as parallel agent sessions make, all run,
// and leave one verified binary behind.
func TestLauncherFirstRunsAtOnceAllSucceed(t *testing.T) {
	needSh(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 300*time.Millisecond)
	results := make([]result, 4)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = r.sh(t, nil, "help")
		}()
	}
	wg.Wait()
	for i, got := range results {
		if got.code != 0 || !strings.Contains(got.stdout, "Exit codes:") {
			t.Errorf("run %d: exit %d\n%s", i, got.code, got.stderr)
		}
	}
	if left := filesUnder(t, r.cache); len(left) != 1 || left[0] != r.installed() {
		t.Errorf("cache holds %v, want only %s", left, r.installed())
	}
}

// Installing a release removes older releases' binaries, and nothing else.
func TestLauncherRemovesOtherVersionsAfterAnInstall(t *testing.T) {
	needSh(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 0)
	cache := filepath.Join(r.cache, "proving-it-works")
	old := filepath.Join(cache, sha256Hex([]byte("an older release")))
	keep := []string{filepath.Join(cache, "source"), filepath.Join(cache, "not-a-hash")}
	for _, d := range append([]string{old}, keep...) {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		writeText(t, filepath.Join(d, "f"), "x")
	}
	if got := r.sh(t, nil, "help"); got.code != 0 {
		t.Fatalf("exit %d\n%s", got.code, got.stderr)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("the older release is still cached: %v", err)
	}
	for _, d := range keep {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s was removed: %v", d, err)
		}
	}
}

// On Windows the launcher asks for the machine's native architecture, which
// an emulated Git Bash's uname hides.
func TestLauncherAsksWindowsForItsNativeArchitecture(t *testing.T) {
	needSh(t)
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	r := newFakeRelease(t, sha256Hex([]byte("x")), nil, 0)
	writeText(t, filepath.Join(r.bin, "checksums.txt"), r.want+"  movie-windows-arm64.exe\n")
	// The runner is 64-bit, so PROCESSOR_ARCHITEW6432 is unset. (Setting it
	// empty instead makes Git Bash drop PROCESSOR_ARCHITECTURE as well.)
	got := r.sh(t, []string{"PROCESSOR_ARCHITECTURE=ARM64"}, "help")
	if _, ok := r.requested.Load("/v" + testVersion + "/movie-windows-arm64.exe"); !ok {
		t.Errorf("did not ask for movie-windows-arm64.exe (exit %d):\n%s", got.code, got.stderr)
	}
}

// MOVIE_FROM_SOURCE=1 runs this checkout's source, built once and rebuilt
// only when the source changes, for testing unreleased work.
func TestLauncherBuildsTheCheckoutFromSource(t *testing.T) {
	needSh(t)
	if testing.Short() {
		t.Skip("builds movie from source")
	}
	r := &fakeRelease{cache: t.TempDir()}
	launcher := filepath.Join(repoRoot(t), binDir, "movie")
	first := r.run(t, exec.Command("sh", launcher, "help"), []string{"MOVIE_FROM_SOURCE=1"})
	if first.code != 0 || !strings.Contains(first.stdout, "Exit codes:") || !strings.Contains(first.stderr, "building "+hostReleaseName()) {
		t.Fatalf("exit %d\n%s", first.code, first.stderr)
	}
	if _, err := os.Stat(filepath.Join(r.cache, "proving-it-works", "source", hostReleaseName())); err != nil {
		t.Fatalf("not built into the source cache: %v", err)
	}
	again := r.run(t, exec.Command("sh", launcher, "help"), []string{"MOVIE_FROM_SOURCE=1"})
	if again.code != 0 || strings.Contains(again.stderr, "building") {
		t.Errorf("rebuilt unchanged source (exit %d):\n%s", again.code, again.stderr)
	}
}

func needPowerShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("the PowerShell launcher is for Windows")
	}
	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("needs powershell on PATH")
	}
}

// powershell runs movie.ps1 the way SKILL.md says to.
func (r *fakeRelease) powershell(t *testing.T, env []string, args ...string) result {
	t.Helper()
	argv := append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(r.bin, "movie.ps1")}, args...)
	return r.run(t, exec.Command("powershell", argv...), env)
}

// movie.ps1 downloads, verifies, caches, and runs as the sh launcher does,
// into the same cache, so Git Bash then finds it there.
func TestPowerShellLauncherDownloadsIntoTheSharedCache(t *testing.T) {
	needPowerShell(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 0)
	got := r.powershell(t, nil, "a b%c;d")
	if got.code != 2 || !strings.Contains(got.stderr, `unknown command "a b%c;d"`) {
		t.Fatalf("argument did not arrive intact (exit %d):\n%s", got.code, got.stderr)
	}
	if data, err := os.ReadFile(r.installed()); err != nil || sha256Hex(data) != r.want {
		t.Fatalf("not cached at %s: %v", r.installed(), err)
	}
	help := r.powershell(t, nil, "help")
	if help.code != 0 || !strings.Contains(help.stdout, "Exit codes:") {
		t.Errorf("help: exit %d\n%s%s", help.code, help.stdout, help.stderr)
	}
	if _, err := exec.LookPath("sh"); err == nil {
		if got := r.sh(t, nil, "help"); got.code != 0 {
			t.Errorf("Git Bash: exit %d\n%s", got.code, got.stderr)
		}
	}
	if n := r.downloads.Load(); n != 1 {
		t.Errorf("%d downloads, want 1: later runs must use the shared cache", n)
	}
}

func TestPowerShellLauncherRefusesTheWrongChecksum(t *testing.T) {
	needPowerShell(t)
	body := hostBinary(t)
	wrong := sha256Hex([]byte("not the release"))
	r := newFakeRelease(t, wrong, body, 0)
	got := r.powershell(t, nil, "help")
	if got.code != 2 || !strings.Contains(got.stderr, sha256Hex(body)) || !strings.Contains(got.stderr, wrong) {
		t.Fatalf("want exit 2 naming both hashes, got %d:\n%s", got.code, got.stderr)
	}
	if left := filesUnder(t, r.cache); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

func TestPowerShellLauncherSaysWhatToFetchWhenTheDownloadFails(t *testing.T) {
	needPowerShell(t)
	want := sha256Hex(hostBinary(t))
	r := newFakeRelease(t, want, nil, 0)
	got := r.powershell(t, nil, "help")
	url := r.url + "/v" + testVersion + "/" + hostReleaseName()
	if got.code != 2 || !strings.Contains(got.stderr, "could not download "+url) || !strings.Contains(got.stderr, "SHA-256 must be "+want) {
		t.Fatalf("exit %d:\n%s", got.code, got.stderr)
	}
}

func TestPowerShellLauncherFirstRunsAtOnceAllSucceed(t *testing.T) {
	needPowerShell(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 300*time.Millisecond)
	results := make([]result, 4)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = r.powershell(t, nil, "help")
		}()
	}
	wg.Wait()
	for i, got := range results {
		if got.code != 0 {
			t.Errorf("run %d: exit %d\n%s", i, got.code, got.stderr)
		}
	}
	if left := filesUnder(t, r.cache); len(left) != 1 || left[0] != r.installed() {
		t.Errorf("cache holds %v, want only %s", left, r.installed())
	}
}

// movie.ps1 hands movie its arguments exactly, which PowerShell 5.1's own
// native-command call does not: it drops empty ones and splits on quotes.
func TestPowerShellLauncherPassesQuotesAndEmptyArgumentsThrough(t *testing.T) {
	needPowerShell(t)
	body := hostBinary(t)
	r := newFakeRelease(t, sha256Hex(body), body, 0)
	for _, arg := range []string{`say "hi" \\ \"`, ""} {
		got := r.powershell(t, nil, arg)
		if want := fmt.Sprintf("unknown command %q", arg); got.code != 2 || !strings.Contains(got.stderr, want) {
			t.Errorf("want %s, got exit %d:\n%s", want, got.code, got.stderr)
		}
	}
}

// A download interrupted earlier leaves a temporary file behind; a failed
// download must still end with the fetch instructions, not a prompt.
func TestPowerShellLauncherFailsCleanlyBesideALeftoverDownload(t *testing.T) {
	needPowerShell(t)
	want := sha256Hex(hostBinary(t))
	r := newFakeRelease(t, want, nil, 0)
	dir := filepath.Join(r.cache, "proving-it-works", want)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(dir, "."+hostReleaseName()+".999"), "partial")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(r.bin, "movie.ps1"), "help")
	got := r.run(t, cmd, nil)
	if got.code != 2 || !strings.Contains(got.stderr, "SHA-256 must be "+want) {
		t.Fatalf("exit %d:\n%s", got.code, got.stderr)
	}
}
