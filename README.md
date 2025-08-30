# syncdir

A tiny, professional-grade directory copy/sync CLI for Windows (works cross‑platform too).
`cp -r` style UX with **differential copy**, **mirror mode (MECE)**, **dry‑run**, and **excludes**.

> Built with Go. Single binary. Safe by default.

---

## Features

- **cp-like UX**: `syncdir cp -r SRC DST`
- **Differential copy** (size + mtime; optional SHA1 checksum)
- **Mirror mode** (`--mirror`): make DST exactly match SRC (delete extras)
- **Dry-run** (`--dry-run`): print planned actions only
- **Exclude patterns** (`--exclude`): `.git`, `*.tmp`, `node_modules`, etc.
- **Safety rails**: prevents nested SRC/DST accidents, same‑path detection
- **Windows-friendly**: path normalization, case-insensitive comparisons
- **Useful exit codes** & consistent, helpful usage on errors

---

## Quickstart

```powershell
# 1) Clone or create your project
cd E:\projects\syncdir

# 2) Initialize module (first time only)
go mod init syncdir

# 3) Put the provided main.go into this folder, then build:
go build -o syncdir.exe

# 4) Get help
.\syncdir.exe --help
.\syncdir.exe help cp

# 5) Dry-run (safe preview)
.\syncdir.exe cp -r --dry-run "E:\dotinstall" "C:\Users\ckklu\dotinstall"

# 6) Differential copy
.\syncdir.exe cp -r "E:\dotinstall" "C:\Users\ckklu\dotinstall"

# 7) Mirror mode (MECE: delete extras in DST)
.\syncdir.exe cp -r --mirror "E:\dotinstall" "C:\Users\ckklu\dotinstall"

# 8) Exclude examples
.\syncdir.exe cp -r --exclude ".git" --exclude "*.tmp" "E:\src" "E:\dst"
```

> Tip: Always try `--dry-run` before a destructive `--mirror` operation.

---

## Usage

### Global

```
syncdir - simple cp -r / mirroring sync for Windows

Usage:
  syncdir <command> [options]

Commands:
  cp           Copy/sync files and directories
  help         Show help (alias: -h, --help)
  version      Show version

Global Options:
  -h, --help   Show this help
  --version    Show version
```

### `cp` Subcommand

```
syncdir cp - copy/sync

Usage:
  syncdir cp -r [--mirror] [--dry-run] [--exclude PATTERN ...] [--verbose] [--checksum] SRC DST

Options:
  -r             Recursive (required when SRC is a directory)
  --mirror       Mirror mode (delete files/dirs not present in SRC)
  --dry-run      Show actions without changing anything
  --exclude X    Exclude pattern (can repeat) e.g. ".git", "*.tmp", "node_modules"
  --verbose      Verbose logging
  --checksum     Use SHA1 to decide copy (slower, safer)
  --help         Show this help for 'cp'
```

#### Examples

```
syncdir cp -r "E:\dotinstall" "C:\Users\ckklu\dotinstall"
syncdir cp -r --mirror "E:\dotinstall" "C:\Users\ckklu\dotinstall"
syncdir cp -r --dry-run --exclude ".git" --exclude "*.tmp" "E:\src" "E:\dst"
```

---

## Behavior & Design Notes

### Differential Copy
- By default, syncdir compares **size & mtime (±1s tolerance)** to decide if a file needs copying.
- Use `--checksum` to add a **SHA1** equality check for extra safety (slower).

### Mirror Mode (MECE)
- With `--mirror`, **DST is made to exactly match SRC**.
- Files/dirs present only in DST will be **deleted**.
- _Strongly_ recommended to preview with `--dry-run` first.

### Exclude Patterns
- `--exclude` accepts wildcard patterns with `filepath.Match` semantics.
- Typical patterns:
  - Folder by name: `.git`, `node_modules`, `dist`
  - Wildcards: `*.tmp`, `*.log`, `*.bak`
- Excludes are applied both when **copying** and when checking **mirror deletions**.

### Safety Rails
- **Same-path guard**: refuses when SRC and DST resolve to the same path.
- **Nest guards**: refuses when DST is inside SRC (or vice‑versa). Prevents recursive disasters.
- **Dry-run everywhere**: all operations (create, copy, delete) can be previewed.

### Exit Codes
- `0` — success
- `1` — runtime error (I/O, permissions, etc.)
- `2` — usage error (bad flags/args, missing SRC/DST, forbidden path relations)

---

## Windows Tips

- Always quote paths containing spaces: `"C:\Users\Name\My Folder"`
- Use forward or back slashes; Go normalizes internally, but be consistent.
- On NTFS, timestamp granularity can vary; the ±1s tolerance keeps copies stable.

---

## Build, Test, Release

### Build (Windows)
```powershell
go build -o syncdir.exe
```

### Cross-compile (examples)
```powershell
# Linux amd64
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o syncdir-linux-amd64
# macOS arm64
$env:GOOS="darwin"; $env:GOARCH="arm64"; go build -o syncdir-darwin-arm64
# reset
Remove-Item Env:GOOS, Env:GOARCH
```

### Run Unit Tests (if you add them later)
```powershell
go test ./...
```

### Suggested Release Steps
1. Update `appVersion` constant in `main.go`.
2. `git tag vX.Y.Z && git push --tags`
3. Attach binaries (`.exe`, etc.) to the GitHub Release.
4. Include a CHANGELOG entry (see below).

---

## Roadmap Ideas

- `--parallel N` for concurrent copies (small files speed‑up)
- `--progress` with per‑file and overall progress bars
- `--size-only` / `--mtime-only` / `--no-preserve-times`
- Logging to file, `--quiet`, JSON output mode
- POSIX ACLs/attributes (platform‑specific)
- Integration tests on Windows CI (GitHub Actions)

---

## Contributing

PRs welcome! Please keep code **small, readable, and safe-by-default**.

1. Fork & branch: `feat/...` or `fix/...`
2. `go fmt`, `go vet`, and add tests for new logic
3. Open PR with a concise description and examples

---

## License

MIT © 2025 Pinkgold-Tech (Ken)

---

## Changelog (template)

### [v0.2.0] - 2025-08-31
- Add professional CLI surface: global `help`, `version`, `help cp`
- Stronger validations and exit codes
- Improved usage on errors
- Baseline copy/sync/mirror/exclude/dry‑run

### [v0.1.0] - 2025-08-31
- Initial working prototype
