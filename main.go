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

const (
	appName    = "syncdir"
	appVersion = "0.2.0" // bump as you publish to GitHub

	// Exit codes (loosely inspired by sysexits and common CLI conventions)
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
	return fmt.Sprintf(`%s %s - copy/sync

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
`, appName, "cp", appName, appName, appName, appName)
}

/* =========================
          MAIN
========================= */

func main() {
	// global flags
	var (
		showHelp    bool
		showVersion bool
	)

	// We manually parse global flags to keep subcommand UX clean
	if len(os.Args) < 2 {
		printErr(globalUsage())
		os.Exit(exitUsage)
	}

	switch os.Args[1] {
	case "-h", "--help", "help":
		// `syncdir help` or `syncdir --help`
		if len(os.Args) >= 3 {
			switch os.Args[2] {
			case "cp":
				printErr(cpUsage())
			default:
				printErr(globalUsage())
				printErr(fmt.Sprintf("Unknown topic for help: %q\n", os.Args[2]))
			}
			os.Exit(exitUsage)
		}
		printErr(globalUsage())
		os.Exit(exitUsage)

	case "--version", "version":
		fmt.Printf("%s %s (%s/%s)\n", appName, appVersion, runtime.GOOS, runtime.GOARCH)
		os.Exit(exitOK)

	case "cp":
		runCp(os.Args[2:]) // cp has its own FlagSet
		os.Exit(exitOK)

	default:
		// Could be global flags mixed (rare). Provide lenient handling:
		for _, a := range os.Args[1:] {
			if a == "--version" {
				showVersion = true
			}
			if a == "-h" || a == "--help" {
				showHelp = true
			}
		}
		if showVersion {
			fmt.Printf("%s %s (%s/%s)\n", appName, appVersion, runtime.GOOS, runtime.GOARCH)
			os.Exit(exitOK)
		}
		if showHelp {
			printErr(globalUsage())
			os.Exit(exitUsage)
		}
		printErr(globalUsage())
		printErr(fmt.Sprintf("Unknown command: %q\n", os.Args[1]))
		os.Exit(exitUsage)
	}
}

/* =========================
        SUBCOMMAND: cp
========================= */

func runCp(args []string) {
	fs := flag.NewFlagSet("cp", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress default usage prints; we print our own

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

	// Parse
	if err := fs.Parse(args); err != nil {
		// parsing error -> show cp usage
		printErr(cpUsage())
		printErr(fmt.Sprintf("Argument error: %v\n", err))
		os.Exit(exitUsage)
	}
	opt.excludes = exc

	if wantHelp {
		printErr(cpUsage())
		os.Exit(exitUsage)
	}

	// Positional args
	rest := fs.Args()
	if len(rest) != 2 {
		printErr(cpUsage())
		printErr("error: need SRC and DST\n")
		os.Exit(exitUsage)
	}

	src, dst := filepath.Clean(rest[0]), filepath.Clean(rest[1])

	// Validations
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

	// same path?
	if samePath(absSrc, absDst) {
		dieUsagef("error: SRC and DST are the same path:\n  %s\n", absSrc)
	}
	// prevent nesting accidents
	if isSubpath(absDst, absSrc) {
		dieUsagef("error: DST is inside SRC; refused to prevent recursion:\n  DST=%s inside SRC=%s\n", absDst, absSrc)
	}
	if isSubpath(absSrc, absDst) {
		dieUsagef("error: SRC is inside DST; refused to prevent recursion:\n  SRC=%s inside DST=%s\n", absSrc, absDst)
	}

	// Execute
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

	// forward pass: create/update
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

	// mirror pass: remove extras in dst
	if opt.mirror {
		err = filepath.WalkDir(dst, func(dstPath string, d fs.DirEntry, walkErr error) error {
			if walkErr !=
