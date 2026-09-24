# Portable movie regressions

Run a suite from the repository root:

```sh
uv run --script tests/proving-it-works-with-a-movie/run-tests.py --suite browser
uv run --script tests/proving-it-works-with-a-movie/run-tests.py --suite contracts
uv run --script tests/proving-it-works-with-a-movie/run-tests.py --suite terminal
uv run --script tests/proving-it-works-with-a-movie/run-tests.py --suite all
```

The movie pipeline itself is tested in Go: `go test ./...`.

By default, unavailable external capabilities are reported as skips. Add
`--require-capabilities` when the selected environment is required to provide
them; any skip then makes the run fail.

The `contracts` suite is the safe entrypoint for mocked process/media boundaries
and text fixtures. It does not run the existing media/session suites, inspect a
finished movie, or replace live acceptance and the required human viewing gate.

The `terminal` suite starts a real ttyd session and skips where ttyd or a
Chrome-family browser is missing. It films `bash` by default on Unix and
`powershell51` on Windows; set `MOVIE_TEST_SHELL` to `powershell51`,
`powershell7`, `gitbash`, or `bash`, and `MOVIE_TEST_SHELL_EXE`,
`MOVIE_TEST_TTYD`, or `MOVIE_TEST_BROWSER` when those executables are not on
PATH.
