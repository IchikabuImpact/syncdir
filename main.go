package main

import (
	"bufio"
	"crypto/sha1"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

/*
syncdir - professional single-binary directory sync

Highlights:
- Global help/version and `help <command>`
- cp subcommand with --help
- Strong validations (args, path relationships, src existence, recursion safety)
- Proper exit codes, stderr usage, and consistent usage printing
*/

var (
	exitFn           = func(code int) { os.Exit(code) }
	stderr io.Writer = os.Stderr // ★ io.Writer にする（重要）
)

func printErr(s string) { _, _ = fmt.Fprint(stderr, s) }

const (
	appName    = "syncdir"
	appVersion = "0.2.0"

	exitOK           = 0
	exitUsage        = 2
	exitRuntimeError = 1
)

type options struct {
	recursive bool
	mirror    bool
	dryRun    bool
	excludes  []string
	verbose   bool
	checksum  bool
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

/* =========================
          USAGE
========================= */

func globalUsage() string {
	return fmt.Sprintf(`%s - simple cp -r / mirroring sync for Windows

Usage:
  %s <command> [options]

Commands:
  cp           Copy/sync files and directories
  help         Show help (alias: -h, --help)
  version      Show version

Global Options:
  -h, --help   Show this help
  --version    Show version

See:
  %s help cp
`, appName, appName, appName)
}

func cpUsage() string {
	return fmt.Sprintf(`%s cp - copy/sync

Usage:
  %s cp -r [--mirror] [--dry-run] [--exclude PATTERN ...] [--verbose] [--checksum] SRC DST

Options:
  -r             Recursive (required when SRC is a directory)
  --mirror       Mirror mode (delete files/dirs not present in SRC)
  --dry-run      Show actions without changing anything
  --exclude X    Exclude pattern (can repeat) e.g. ".git", "*.tmp", "node_modules"
  --verbose      Verbose logging
  --checksum     Use SHA1 to decide copy (slower, safer)
  --help         Show this help for 'cp'

Examples:
  %s cp -r "E:\dotinstall" "C:\Users\ckklu\dotinstall"
  %s cp -r --mirror "E:\dotinstall" "C:\Users\ckklu\dotinstall"
  %s cp -r --dry-run --exclude ".git" --exclude "*.tmp" "E:\src" "E:\dst"
`, appName, appName, appName, appName, appName)
}

/* =========================
          MAIN
========================= */

func main() {
	if len(os.Args) < 2 {
		printErr(globalUsage())
		exitFn(exitUsage)
	}

	switch os.Args[1] {
	case "-h", "--help", "help":
		if len(os.Args) >= 3 {
			switch os.Args[2] {
			case "cp":
				printErr(cpUsage())
			default:
				printErr(globalUsage())
				printErr(fmt.Sprintf("Unknown topic for help: %q\n", os.Args[2]))
			}
			exitFn(exitUsage)
		}
		printErr(globalUsage())
		exitFn(exitUsage)

	case "--version", "version":
		fmt.Printf("%s %s (%s/%s)\n", appName, appVersion, runtime.GOOS, runtime.GOARCH)
		exitFn(exitOK)

	case "cp":
		runCp(os.Args[2:])
		exitFn(exitOK)

	default:
		// fallback: honor --help / --version anywhere
		for _, a := range os.Args[1:] {
			if a == "--version" {
				fmt.Printf("%s %s (%s/%s)\n", appName, appVersion, runtime.GOOS, runtime.GOARCH)
				exitFn(exitOK)
			}
			if a == "-h" || a == "--help" {
				printErr(globalUsage())
				exitFn(exitUsage)
			}
		}
		printErr(globalUsage())
		printErr(fmt.Sprintf("Unknown command: %q\n", os.Args[1]))
		exitFn(exitUsage)
	}
}

/* =========================
        SUBCOMMAND: cp
========================= */

func runCp(args []string) {
	fs := flag.NewFlagSet("cp", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress default prints; we print our own

	var opt options
	var wantHelp bool

	fs.BoolVar(&opt.recursive, "r", false, "recursive copy for directories (required if SRC is dir)")
	fs.BoolVar(&opt.mirror, "mirror", false, "mirror mode (delete files/dirs not present in SRC)")
	fs.BoolVar(&opt.dryRun, "dry-run", false, "show actions without changing anything")
	fs.BoolVar(&opt.verbose, "verbose", false, "verbose logging")
	fs.BoolVar(&opt.checksum, "checksum", false, "use SHA1 checksum to decide copy (slower, safer)")
	exc := multiFlag{}
	fs.Var(&exc, "exclude", "exclude pattern (repeatable)")
	fs.BoolVar(&wantHelp, "help", false, "show help for cp")

	if err := fs.Parse(args); err != nil {
		printErr(cpUsage())
		printErr(fmt.Sprintf("Argument error: %v\n", err))
		exitFn(exitUsage)
	}
	opt.excludes = exc

	if wantHelp {
		printErr(cpUsage())
		exitFn(exitUsage)
	}

	rest := fs.Args()
	if len(rest) != 2 {
		printErr(cpUsage())
		printErr("error: need SRC and DST\n")
		exitFn(exitUsage)
	}
	src, dst := filepath.Clean(rest[0]), filepath.Clean(rest[1])

	srcInfo, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			dieUsagef("error: SRC does not exist: %s\n", src)
		}
		dieRuntime(err)
	}
	if srcInfo.IsDir() && !opt.recursive {
		dieUsagef("error: SRC is a directory; specify -r for recursive copy\n")
	}

	absSrc, _ := filepath.Abs(src)
	absDst, _ := filepath.Abs(dst)

	if samePath(absSrc, absDst) {
		dieUsagef("error: SRC and DST are the same path:\n  %s\n", absSrc)
	}
	if isSubpath(absDst, absSrc) {
		dieUsagef("error: DST is inside SRC; refused to prevent recursion:\n  DST=%s inside SRC=%s\n", absDst, absSrc)
	}
	if isSubpath(absSrc, absDst) {
		dieUsagef("error: SRC is inside DST; refused to prevent recursion:\n  SRC=%s inside DST=%s\n", absSrc, absDst)
	}

	if srcInfo.IsDir() {
		if err := syncDir(src, dst, opt); err != nil {
			dieRuntime(err)
		}
	} else {
		if err := copyOneFile(src, dst, opt); err != nil {
			dieRuntime(err)
		}
	}

	if opt.dryRun {
		logAlways("[DRY-RUN] no changes were made.")
	}
}

/* =========================
         CORE LOGIC
========================= */

func syncDir(src, dst string, opt options) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	// forward pass
	err := filepath.WalkDir(src, func(srcPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(src, srcPath)
		if rel == "." {
			return ensureDir(dst, opt)
		}
		if shouldExclude(rel, d, opt.excludes) {
			if opt.verbose {
				logf("exclude: %s", rel)
			}
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		dstPath := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return ensureDir(dstPath, opt)
		}
		return syncFile(srcPath, dstPath, info, opt)
	})
	if err != nil {
		return err
	}

	// mirror pass
	if opt.mirror {
		err = filepath.WalkDir(dst, func(dstPath string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, _ := filepath.Rel(dst, dstPath)
			if rel == "." {
				return nil
			}
			if shouldExclude(rel, d, opt.excludes) {
				if opt.verbose {
					logf("mirror-skip (excluded): %s", rel)
				}
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			srcPath := filepath.Join(src, rel)
			_, err := os.Lstat(srcPath)
			if err == nil {
				return nil
			}
			if os.IsNotExist(err) {
				if d.IsDir() {
					return removePath(dstPath, true, opt)
				}
				return removePath(dstPath, false, opt)
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func ensureDir(path string, opt options) error {
	if opt.dryRun {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			logf("[DRY] MKDIR %s", path)
		}
		return nil
	}
	return os.MkdirAll(path, 0o755)
}

func syncFile(srcPath, dstPath string, srcInfo fs.FileInfo, opt options) error {
	if dstInfo, err := os.Stat(dstPath); err == nil && dstInfo.Mode().IsRegular() {
		same, err := sameFile(srcPath, dstPath, srcInfo, dstInfo, opt)
		if err != nil {
			return err
		}
		if same {
			if opt.verbose {
				logf("skip (same): %s", dstPath)
			}
			return nil
		}
	}
	return copyOneFile(srcPath, dstPath, opt)
}

func copyOneFile(srcPath, dstPath string, opt options) error {
	if opt.dryRun {
		logf("[DRY] COPY %s -> %s", srcPath, dstPath)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}

	sf, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer sf.Close()

	si, err := sf.Stat()
	if err != nil {
		return err
	}

	df, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, si.Mode())
	if err != nil {
		return err
	}
	buf := bufio.NewWriterSize(df, 2<<20)
	if _, err := io.Copy(buf, sf); err != nil {
		_ = df.Close()
		return err
	}
	if err := buf.Flush(); err != nil {
		_ = df.Close()
		return err
	}
	if err := df.Close(); err != nil {
		return err
	}

	mt := si.ModTime()
	return os.Chtimes(dstPath, mt, mt)
}

func removePath(path string, isDir bool, opt options) error {
	if opt.dryRun {
		if isDir {
			logf("[DRY] RMDIR %s", path)
		} else {
			logf("[DRY] DEL   %s", path)
		}
		return nil
	}
	if isDir {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func sameFile(srcPath, dstPath string, si, di fs.FileInfo, opt options) (bool, error) {
	if si.Size() == di.Size() && absDuration(si.ModTime().Sub(di.ModTime())) <= time.Second {
		if !opt.checksum {
			return true, nil
		}
		sh1, err := sha1sum(srcPath)
		if err != nil {
			return false, err
		}
		dh1, err := sha1sum(dstPath)
		if err != nil {
			return false, err
		}
		return sh1 == dh1, nil
	}
	if opt.checksum {
		sh1, err := sha1sum(srcPath)
		if err != nil {
			return false, err
		}
		dh1, err := sha1sum(dstPath)
		if err != nil {
			return false, err
		}
		return sh1 == dh1, nil
	}
	return false, nil
}

func sha1sum(path string) ([20]byte, error) {
	var zero [20]byte
	f, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return zero, err
	}
	var out [20]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

/* =========================
        HELPERS/UTIL
========================= */

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func shouldExclude(rel string, d fs.DirEntry, patterns []string) bool {
	base := filepath.Base(rel)
	for _, p := range patterns {
		if match, _ := filepath.Match(p, base); match {
			return true
		}
		if p == rel || strings.Contains(rel, string(os.PathSeparator)+p+string(os.PathSeparator)) {
			return true
		}
		if strings.HasPrefix(rel, p+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func isSubpath(child, parent string) bool {
	c := strings.ToLower(filepath.Clean(child))
	p := strings.ToLower(filepath.Clean(parent))
	if c == p {
		return false
	}
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func logf(format string, args ...any) { fmt.Printf(format+"\n", args...) }
func logAlways(msg string)            { fmt.Println(msg) }

func dieRuntime(err error) {
	printErr(fmt.Sprintf("error: %v\n", err))
	exitFn(exitRuntimeError)
}

func dieUsagef(format string, a ...any) {
	printErr(cpUsage())
	printErr(fmt.Sprintf(format, a...))
	exitFn(exitUsage)
}
