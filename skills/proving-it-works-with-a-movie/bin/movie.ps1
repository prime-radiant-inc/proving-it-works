# Run movie, downloading this machine's release binary on first use.
#
# Windows PowerShell's default execution policy blocks scripts, so run it as
#   powershell -ExecutionPolicy Bypass -File "$SKILL_DIR\bin\movie.ps1" ARGS...
#
# The binary for the version in VERSION comes from that GitHub release, is
# checked against checksums.txt, and is cached at
#   %LOCALAPPDATA%\proving-it-works\SHA256\movie-windows-ARCH.exe
# keyed by its expected hash: the same place bin/movie uses from Git Bash.
# Arguments and the exit code pass through.
#
#   MOVIE_RELEASE_URL=URL  download URL/vVERSION/NAME instead of from GitHub
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Fail([string]$message) {
    [Console]::Error.WriteLine("movie: $message")
    exit 2
}

# Remove-EmptyDirectory removes a directory only when nothing is in it: a
# leftover or another run's download may be, and removing a directory with
# contents would stop to ask for confirmation.
function Remove-EmptyDirectory([string]$path) {
    if (-not (Get-ChildItem -LiteralPath $path -Force -ErrorAction SilentlyContinue)) {
        Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue
    }
}

# Quote-Argument quotes one argument for a Windows command line, so the
# program's argument parser gets it back exactly (CommandLineToArgvW rules).
function Quote-Argument([string]$arg) {
    if ($arg -ne '' -and $arg -notmatch '[\s"]') { return $arg }
    $quoted = '"'
    $slashes = 0
    foreach ($c in $arg.ToCharArray()) {
        if ($c -eq '\') { $slashes++; continue }
        if ($c -eq '"') { $quoted += ('\' * (2 * $slashes + 1)) + '"' }
        else { $quoted += ('\' * $slashes) + $c }
        $slashes = 0
    }
    $quoted + ('\' * (2 * $slashes)) + '"'
}

$argv = $args

$arch = $env:PROCESSOR_ARCHITEW6432
if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
$name = if ($arch -eq 'ARM64') { 'movie-windows-arm64.exe' } else { 'movie-windows-amd64.exe' }

$version = (Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot 'VERSION')).Trim()
$want = $null
foreach ($line in Get-Content -LiteralPath (Join-Path $PSScriptRoot 'checksums.txt')) {
    $fields = $line.Trim() -split '\s+'
    if ($fields.Count -ge 2 -and $fields[1] -eq $name) { $want = $fields[0].ToLower() }
}
if (-not $want) { Fail "checksums.txt has no line for $name" }

$cache = Join-Path $env:LOCALAPPDATA 'proving-it-works'
try {
    New-Item -ItemType Directory -Force -Path $cache | Out-Null
} catch {
    $cache = Join-Path ([IO.Path]::GetTempPath()) 'proving-it-works'
    [Console]::Error.WriteLine("movie: cannot write to $env:LOCALAPPDATA; caching in $cache instead")
    New-Item -ItemType Directory -Force -Path $cache | Out-Null
}
$dir = Join-Path $cache $want
$bin = Join-Path $dir $name

if (-not (Test-Path -LiteralPath $bin)) {
    $base = if ($env:MOVIE_RELEASE_URL) { $env:MOVIE_RELEASE_URL } else { 'https://github.com/prime-radiant-inc/proving-it-works/releases/download' }
    $url = "$base/v$version/$name"
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $tmp = Join-Path $dir ".$name.$PID"
    [Console]::Error.WriteLine("movie: downloading $name $version (first run only)")
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $tmp
    } catch {
        Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
        Remove-EmptyDirectory $dir
        Fail ("could not download $url`n" +
            "  Fetch it another way and put it at $bin`n" +
            "  Its SHA-256 must be $want.")
    }
    $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $tmp).Hash.ToLower()
    if ($got -ne $want) {
        Remove-Item -LiteralPath $tmp -Force
        Remove-EmptyDirectory $dir
        Fail "$url has SHA-256 $got, but checksums.txt says $want; not running it"
    }
    # another first run may have installed it meanwhile; either copy is verified
    try {
        Move-Item -LiteralPath $tmp -Destination $bin
    } catch {
        Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
    }
    if (-not (Test-Path -LiteralPath $bin)) { Fail "could not install $bin" }
    Get-ChildItem -LiteralPath $cache -Directory |
        Where-Object { $_.Name -match '^[0-9a-f]{64}$' -and $_.Name -ne $want } |
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
}

# PowerShell 5.1's own call operator drops empty arguments and splits ones
# with quotes, so build movie's command line and start it directly; it
# shares this console's input and output.
$start = New-Object System.Diagnostics.ProcessStartInfo $bin
$start.Arguments = (@(foreach ($a in $argv) { Quote-Argument $a })) -join ' '
$start.UseShellExecute = $false
$movie = [System.Diagnostics.Process]::Start($start)
$movie.WaitForExit()
exit $movie.ExitCode
