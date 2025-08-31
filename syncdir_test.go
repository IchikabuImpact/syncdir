package main

import (
	"crypto/sha1"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"errors"
)

// ---------- helpers ----------

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// ---------- unit: path relations ----------

func TestIsSubpathAndSamePath(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	sibling := filepath.Join(root, "sibling")

	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}

	absRoot, _ := filepath.Abs(root)
	absChild, _ := filepath.Abs(child)
	absSibling, _ := filepath.Abs(sibling)

	if !isSubpath(absChild, absRoot) {
		t.Fatalf("expected %q to be subpath of %q", absChild, absRoot)
	}
	if isSubpath(absSibling, absChild) {
		t.Fatalf("did not expect %q to be subpath of %q", absSibling, absChild)
	}
	if samePath(absRoot, strings.Clone(absRoot)) != true {
		t.Fatalf("samePath should be true for identical paths")
	}

	// Case-insensitive check (Windows前提だが、関数は小文字化して比較している)
	if runtime.GOOS == "windows" {
		upper := strings.ToUpper(absRoot)
		if !samePath(absRoot, upper) {
			t.Fatalf("samePath should treat case-insensitively on Windows")
		}
	}
}

func TestShouldExclude(t *testing.T) {
	patterns := []string{".git", "*.tmp", "node_modules"}

	yes := []string{
		filepath.Join("foo", ".git"),
		filepath.Join("foo", "node_modules", "pkg", "index.js"),
		filepath.Join("foo", "bar.tmp"),
	}
	no := []string{
		filepath.Join("foo", ".gitignore"),
		filepath.Join("foo", "bar.txt"),
	}

	for _, rel := range yes {
		if !shouldExclude(rel, nil, patterns) {
			t.Fatalf("shouldExclude(%q) = false, want true", rel)
		}
	}
	for _, rel := range no {
		if shouldExclude(rel, nil, patterns) {
			t.Fatalf("shouldExclude(%q) = true, want false", rel)
		}
	}
}

// ---------- unit: primitives ----------

func TestAbsDuration(t *testing.T) {
	if absDuration(-5*time.Second) != 5*time.Second {
		t.Fatal("absDuration failed for negative duration")
	}
	if absDuration(3*time.Second) != 3*time.Second {
		t.Fatal("absDuration failed for positive duration")
	}
}

func TestSha1sum(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.txt")
	data := []byte("hello syncdir")
	writeFile(t, f, data)

	got, err := sha1sum(f)
	if err != nil {
		t.Fatalf("sha1sum: %v", err)
	}
	want := sha1.Sum(data)
	if got != want {
		t.Fatalf("sha1 mismatch: got %x want %x", got, want)
	}
}

// ---------- unit/integration: file equality & copy ----------

func TestSameFileAndCopyOneFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	writeFile(t, src, []byte("abc"))
	// 初回コピー
	if err := copyOneFile(src, dst, options{}); err != nil {
		t.Fatalf("copyOneFile: %v", err)
	}
	// 同一判定（サイズ＆mtime）
	si, _ := os.Stat(src)
	di, _ := os.Stat(dst)
	same, err := sameFile(src, dst, si, di, options{})
	if err != nil {
		t.Fatalf("sameFile: %v", err)
	}
	if !same {
		t.Fatalf("sameFile should be true right after copy")
	}

	// 中身を更新 → same=false
	time.Sleep(2 * time.Second) // CIでの時間解像度/負荷に余裕を持たせる
	writeFile(t, src, []byte("abcd"))
	si, _ = os.Stat(src)
	di, _ = os.Stat(dst)
	same, _ = sameFile(src, dst, si, di, options{})
	if same {
		t.Fatalf("sameFile should be false after content change")
	}

	// checksum オプションでも検証
	same, _ = sameFile(src, dst, si, di, options{checksum: true})
	if same {
		t.Fatalf("sameFile(checksum) should be false after content change")
	}
}

// ---------- integration: syncDir copy & mirror ----------

func TestSyncDir_CopyAndMirror(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// SRCにファイル群を作成
	writeFile(t, filepath.Join(src, "a.txt"), []byte("hello"))
	writeFile(t, filepath.Join(src, "dir1", "b.txt"), []byte("world"))
	writeFile(t, filepath.Join(src, "node_modules", "skip.txt"), []byte("skip me"))

	// 1回目：差分コピー + 除外
	opt := options{recursive: true, mirror: false, dryRun: false, excludes: []string{"node_modules"}}
	if err := syncDir(src, dst, opt); err != nil {
		t.Fatalf("syncDir(copy): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
		t.Fatalf("a.txt not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "dir1", "b.txt")); err != nil {
		t.Fatalf("dir1/b.txt not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "node_modules", "skip.txt")); !os.IsNotExist(err) {
		t.Fatalf("excluded file should not exist in dst")
	}

	// DSTだけに余分を作っておく
	writeFile(t, filepath.Join(dst, "extra.txt"), []byte("remove me"))

	// 2回目：ミラーで余分削除（除外は尊重）
	opt = options{recursive: true, mirror: true, dryRun: false, excludes: []string{"node_modules"}}
	if err := syncDir(src, dst, opt); err != nil {
		t.Fatalf("syncDir(mirror): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("extra.txt should be removed in mirror mode")
	}
}

// ---------- unit: ensureDir dry-run ----------

func TestEnsureDir_DryRun(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "newdir")

	// dry-runなら作られない
	if err := ensureDir(target, options{dryRun: true}); err != nil {
		t.Fatalf("ensureDir(dry-run) error: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("directory should not be created in dry-run")
	}
}

func TestMain_HelpAndVersion(t *testing.T) {
    // --help は usage を出して exit 2
    code, errOut := runWithIntercept(t, []string{"--help"}, func() { main() })
    if code != exitUsage || !strings.Contains(errOut, "Usage:") {
        t.Fatalf("--help: want code=%d and usage in stderr, got code=%d, stderr=%q", exitUsage, code, errOut)
    }

    // version は exit 0（stderr ではなく stdout に出るのでコードのみ検査）
    code, _ = runWithIntercept(t, []string{"version"}, func() { main() })
    if code != exitOK {
        t.Fatalf("version: want code=%d, got %d", exitOK, code)
    }

    // 未知コマンドは usage + Unknown command で exit 2
    code, errOut = runWithIntercept(t, []string{"wat"}, func() { main() })
    if code != exitUsage || !strings.Contains(errOut, "Unknown command") {
        t.Fatalf("wat: want code=%d and 'Unknown command', got code=%d, stderr=%q", exitUsage, code, errOut)
    }
}

func TestRunCp_Errors_ShowUsage(t *testing.T) {
    // 引数不足
    code, errOut := runWithIntercept(t, nil, func() { runCp([]string{}) })
    if code != exitUsage || !strings.Contains(errOut, "need SRC and DST") {
        t.Fatalf("need SRC/DST: code=%d stderr=%q", code, errOut)
    }

    tmp := t.TempDir()
    src := filepath.Join(tmp, "srcdir")
    dst := filepath.Join(tmp, "dstdir")
    _ = os.MkdirAll(src, 0o755)

    // -r なしでディレクトリ
    code, errOut = runWithIntercept(t, nil, func() { runCp([]string{src, dst}) })
    if code != exitUsage || !strings.Contains(errOut, "specify -r") {
        t.Fatalf("dir without -r: code=%d stderr=%q", code, errOut)
    }

    // 同一パス
    code, errOut = runWithIntercept(t, nil, func() { runCp([]string{"-r", src, src}) })
    if code != exitUsage || !strings.Contains(errOut, "same path") {
        t.Fatalf("same path: code=%d stderr=%q", code, errOut)
    }

    // 入れ子（DSTがSRCの内側）
    inner := filepath.Join(src, "inner")
    code, errOut = runWithIntercept(t, nil, func() { runCp([]string{"-r", src, inner}) })
    if code != exitUsage || !strings.Contains(errOut, "DST is inside SRC") {
        t.Fatalf("nest: code=%d stderr=%q", code, errOut)
    }
}
func TestSameFile_ChecksumEqual(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir,"a.txt")
	b := filepath.Join(dir,"b.txt")
	os.WriteFile(a, []byte("same"), 0o644)
	os.WriteFile(b, []byte("same"), 0o644)

	si,_ := os.Stat(a); bi,_ := os.Stat(b)
	// 時刻がズレていても checksum なら true を期待
	same, err := sameFile(a, b, si, bi, options{checksum:true})
	if err != nil || !same {
		t.Fatalf("checksum equal should be true, err=%v", err)
	}
}
func runWithIntercept(t *testing.T, args []string, f func()) (code int, errOut string) {
    t.Helper()

    oldExit, oldErr, oldArgs := exitFn, stderr, os.Args
    defer func() { exitFn, stderr, os.Args = oldExit, oldErr, oldArgs }()

    var buf bytes.Buffer
    stderr = &buf
    exitFn = func(c int) { panic(c) }
    os.Args = append([]string{appName}, args...)

    defer func() {
        if r := recover(); r != nil {
            if c, ok := r.(int); ok {
                code = c
            } else {
                t.Fatalf("unexpected panic: %#v", r)
            }
        }
        errOut = buf.String()
    }()

    f() // ここで main() や runCp(...) を直接呼ぶ
    return
}
func TestHelp_TopicCp(t *testing.T) {
    code, errOut := runWithIntercept(t, []string{"help", "cp"}, func() { main() })
    if code != exitUsage || !strings.Contains(errOut, "cp - copy/sync") {
        t.Fatalf("help cp: code=%d stderr=%q", code, errOut)
    }
}
func TestCp_HelpFlag(t *testing.T) {
    code, errOut := runWithIntercept(t, nil, func() { runCp([]string{"--help"}) })
    if code != exitUsage || !strings.Contains(errOut, "cp - copy/sync") {
        t.Fatalf("cp --help: code=%d stderr=%q", code, errOut)
    }
}
func TestMirror_RemoveDir_And_SkipExcludedVerbose(t *testing.T) {
    src := t.TempDir()
    dst := t.TempDir()

    // SRC: a/keep.txt のみ
    writeFile(t, filepath.Join(src, "a", "keep.txt"), []byte("k"))

    // DST: 余分な dir を用意（mirror で消えることを期待）
    writeFile(t, filepath.Join(dst, "b", "extra.txt"), []byte("x"))

    // DST: 除外対象も用意（node_modules）→ mirror時に "mirror-skip (excluded)" の行が実行される
    writeFile(t, filepath.Join(dst, "node_modules", "stay.txt"), []byte("s"))

    opt := options{
        recursive: true,
        mirror:    true,
        dryRun:    false,
        verbose:   true,                      // ← logf の行を実行させる
        excludes:  []string{"node_modules"},  // ← mirror-skip (excluded) を踏む
    }
    if err := syncDir(src, dst, opt); err != nil {
        t.Fatalf("syncDir mirror: %v", err)
    }

    // b/ は削除される
    if _, err := os.Stat(filepath.Join(dst, "b")); !os.IsNotExist(err) {
        t.Fatalf("mirror should remove extra dir 'b'")
    }
    // 除外は残る
    if _, err := os.Stat(filepath.Join(dst, "node_modules", "stay.txt")); err != nil {
        t.Fatalf("excluded path should remain: %v", err)
    }
}
func TestRemovePath_FileAndDir_RealDelete(t *testing.T) {
    base := t.TempDir()

    // file
    f := filepath.Join(base, "x.txt")
    writeFile(t, f, []byte("x"))
    if err := removePath(f, false, options{dryRun: false}); err != nil {
        t.Fatalf("remove file: %v", err)
    }
    if _, err := os.Stat(f); !os.IsNotExist(err) {
        t.Fatalf("file should be removed")
    }

    // dir
    d := filepath.Join(base, "d")
    writeFile(t, filepath.Join(d, "y.txt"), []byte("y"))
    if err := removePath(d, true, options{dryRun: false}); err != nil {
        t.Fatalf("remove dir: %v", err)
    }
    if _, err := os.Stat(d); !os.IsNotExist(err) {
        t.Fatalf("dir should be removed")
    }
}
func TestDieHelpers(t *testing.T) {
    code, errOut := runWithIntercept(t, nil, func() { dieUsagef("oops %d", 1) })
    if code != exitUsage || !strings.Contains(errOut, "cp - copy/sync") || !strings.Contains(errOut, "oops 1") {
        t.Fatalf("dieUsagef: code=%d stderr=%q", code, errOut)
    }

    code, errOut = runWithIntercept(t, nil, func() { dieRuntime(errors.New("boom")) })
    if code != exitRuntimeError || !strings.Contains(errOut, "boom") {
        t.Fatalf("dieRuntime: code=%d stderr=%q", code, errOut)
    }
}