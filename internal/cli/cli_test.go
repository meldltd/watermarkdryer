package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0640); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runJSON(t *testing.T, args ...string) (int, report) {
	t.Helper()
	var out, errout bytes.Buffer
	code := Run(append(args, "--json"), &out, &errout)
	var r report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("invalid report (exit %d): %s %s", code, out.String(), errout.String())
	}
	return code, r
}

func TestDetectRemoveAndIdempotence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	original := "package main\n// hi\u200bthere\r\n"
	write(t, path, original)
	old := time.Unix(1234567890, 0)
	os.Chtimes(path, old, old)
	code, r := runJSON(t, "detect", dir, "--check")
	if code != 1 || r.Summary.WouldChange != 1 || r.Summary.Changed != 0 || read(t, path) != original {
		t.Fatalf("detect mutated or bad report: %d %+v", code, r)
	}
	info, _ := os.Stat(path)
	if !info.ModTime().Equal(old) {
		t.Fatal("detect changed mtime")
	}
	if _, err := os.Stat(path + backupSuffix); !os.IsNotExist(err) {
		t.Fatal("detect made a backup")
	}
	code, r = runJSON(t, "remove", dir)
	if code != 0 || r.Summary.Changed != 1 || read(t, path) != "package main\n// hithere\r\n" || read(t, path+backupSuffix) != original {
		t.Fatalf("bad remove: %d %+v", code, r)
	}
	info, _ = os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0640 {
		t.Fatalf("lost permissions: %v", info.Mode())
	}
	mtime := info.ModTime()
	code, r = runJSON(t, "remove", dir)
	if code != 0 || r.Summary.Changed != 0 || r.Summary.Scanned != 1 {
		t.Fatalf("second run: %d %+v", code, r)
	}
	info, _ = os.Stat(path)
	if !info.ModTime().Equal(mtime) {
		t.Fatal("rewrote clean file")
	}
	code, r = runJSON(t, dir, "--check")
	if code != 0 || r.Summary.WouldChange != 0 {
		t.Fatal("post-removal detection failed")
	}
}

func TestRecursionExclusionsAndOverlappingPaths(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), "a\u200bb")
	write(t, filepath.Join(dir, "sub", "b.txt"), "a\u200bb")
	write(t, filepath.Join(dir, "skip", "c.txt"), "a\u200bb")
	write(t, filepath.Join(dir, ".git", "config"), "a\u200bb")
	code, r := runJSON(t, "detect", dir, filepath.Join(dir, "a.txt"), "--exclude", "skip")
	if code != 0 || r.Summary.Scanned != 2 || r.Summary.WithFindings != 2 {
		t.Fatalf("%d %+v", code, r)
	}
	code, r = runJSON(t, "detect", dir, "--recursive=false")
	if code != 0 || r.Summary.Scanned != 1 {
		t.Fatalf("nonrecursive: %d %+v", code, r)
	}
}

func TestSkipBinaryAndFailMalformedText(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.bin"), "\x00\xff\u200b")
	write(t, filepath.Join(dir, "bad.txt"), "hello\xff")
	write(t, filepath.Join(dir, "ok.txt"), "hi\u200b!")
	code, r := runJSON(t, "remove", dir)
	if code != 2 || r.Summary.Errors != 1 || r.Summary.Skipped != 1 || r.Summary.Changed != 1 {
		t.Fatalf("%d %+v", code, r)
	}
	if read(t, filepath.Join(dir, "bad.txt")) != "hello\xff" || read(t, filepath.Join(dir, "a.bin")) != "\x00\xff\u200b" {
		t.Fatal("damaged file")
	}
}

func TestBackupsNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	write(t, p, "a\u200bb")
	write(t, p+backupSuffix, "precious original")
	code, r := runJSON(t, "remove", p)
	if code != 2 || r.Summary.Changed != 0 || read(t, p) != "a\u200bb" || read(t, p+backupSuffix) != "precious original" {
		t.Fatalf("%d %+v", code, r)
	}
	code, r = runJSON(t, "remove", p, "--backup=false")
	if code != 0 || r.Summary.Changed != 1 || read(t, p) != "ab" || read(t, p+backupSuffix) != "precious original" {
		t.Fatal("explicit no-backup failed")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".watermarkdryer-tmp-") {
			t.Fatal("leaked temporary file")
		}
	}
}

func TestSymlinksAndHardLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix links")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "original.txt")
	write(t, p, "a\u200bb")
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	code, r := runJSON(t, "remove", link)
	if code != 0 || r.Summary.Skipped != 1 || read(t, p) != "a\u200bb" {
		t.Fatalf("symlink followed: %d %+v", code, r)
	}
	hard := filepath.Join(dir, "hard.txt")
	if err := os.Link(p, hard); err != nil {
		t.Fatal(err)
	}
	code, r = runJSON(t, "remove", p)
	if code != 2 || r.Summary.Changed != 0 || read(t, p) != "a\u200bb" {
		t.Fatalf("hard link replaced: %d %+v", code, r)
	}
}

func TestChangedFileNotReplaced(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	write(t, p, "a\u200bb")
	info, _ := os.Stat(p)
	write(t, p, "edited meanwhile")
	if _, err := replaceFile(p, info, []byte("a\u200bb"), []byte("ab"), true); err == nil {
		t.Fatal("overwrote concurrent edit")
	}
	if read(t, p) != "edited meanwhile" {
		t.Fatal("lost edit")
	}
}

func TestWhitespaceCarrierRequiresExplicitRemoval(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.txt")
	input := "Text\t   \t    \t     \t  \r\n"
	write(t, p, input)
	code, r := runJSON(t, "remove", p)
	if code != 0 || r.Summary.WithFindings != 1 || r.Summary.Changed != 0 || read(t, p) != input {
		t.Fatalf("heuristic caused a rewrite: %d %+v", code, r)
	}
	code, r = runJSON(t, "detect", p, "--strip-trailing-whitespace", "--check")
	if code != 1 || r.Summary.WouldChange != 1 || read(t, p) != input {
		t.Fatalf("bad check: %d %+v", code, r)
	}
	code, r = runJSON(t, "remove", p, "--strip-trailing-whitespace")
	if code != 0 || r.Summary.Changed != 1 || read(t, p) != "Text\r\n" || read(t, p+backupSuffix) != input {
		t.Fatalf("bad cleanup: %d %+v", code, r)
	}
}

func TestClaudeCoverageReportedInBothModes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plain.go")
	write(t, p, "package main\n// Ordinary source code.\n")
	for _, mode := range []string{"detect", "remove"} {
		code, r := runJSON(t, mode, p)
		if code != 0 || len(r.Files) != 1 || r.Files[0].ClaudeTextWatermark == nil || r.Files[0].ClaudeTextWatermark.Detected != nil {
			t.Fatalf("missing explicit unknown result: %d %+v", code, r)
		}
		var out, errout bytes.Buffer
		code = Run([]string{mode, p, "--verbose"}, &out, &errout)
		if code != 0 || strings.Count(out.String(), "NOT CHECKED:") != 1 || !strings.Contains(out.String(), "NO SUPPORTED INDICATORS") || strings.Contains(out.String(), "CLEAN ") {
			t.Fatalf("misleading clean report: %s %s", out.String(), errout.String())
		}
	}
}

func TestCLIOptionsAndErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	write(t, p, "a\u200fb\u00a0c")
	code, r := runJSON(t, "--mode=detect", "--path", p, "--check", "--keep-spaces")
	if code != 0 || r.Summary.WithFindings != 1 || r.Summary.WouldChange != 0 {
		t.Fatalf("preserved findings: %d %+v", code, r)
	}
	code, r = runJSON(t, "detect", p, "--max-size=1")
	if code != 0 || r.Summary.Skipped != 1 {
		t.Fatal("size limit failed")
	}
	code, r = runJSON(t, "detect", p+"missing")
	if code != 2 || r.Summary.Errors != 1 {
		t.Fatal("missing path succeeded")
	}
	for _, args := range [][]string{nil, {"--mode=nope", p}, {"remove", "--check", p}, {"detect", "--max-size=0", p}, {"--exclude=[", p}, {"--unknown", p}} {
		var out, errout bytes.Buffer
		if Run(args, &out, &errout) != 2 {
			t.Fatalf("accepted bad flags: %v", args)
		}
	}
	for _, args := range [][]string{{"--help"}, {"--version"}} {
		var out bytes.Buffer
		if Run(args, &out, &out) != 0 {
			t.Fatal(args)
		}
	}
}
