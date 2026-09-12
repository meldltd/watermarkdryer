package cli

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"watermarkdryer/watermark"
)

const Version = "0.3.0"
const backupSuffix = ".watermarkdryer.bak"

//go:embed licenses.txt
var licenses string

type excludes []string

func (e *excludes) String() string { return strings.Join(*e, ",") }
func (e *excludes) Set(s string) error {
	if _, err := filepath.Match(s, ""); err != nil {
		return err
	}
	*e = append(*e, s)
	return nil
}

type fileResult struct {
	Path string `json:"path"`
	watermark.Result
	Status  string `json:"status"`
	Changed bool   `json:"changed"`
	Backup  string `json:"backup,omitempty"`
	Error   string `json:"error,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type summary struct {
	Scanned      int `json:"scanned"`
	WithFindings int `json:"with_findings"`
	WouldChange  int `json:"would_change"`
	Changed      int `json:"changed"`
	Skipped      int `json:"skipped"`
	Errors       int `json:"errors"`
}

type report struct {
	Version string       `json:"version"`
	Mode    string       `json:"mode"`
	Files   []fileResult `json:"files"`
	Summary summary      `json:"summary"`
}

// Run implements the CLI without global flags or process exits, so the same
// filesystem behavior can be tested as a built executable or in-process.
func Run(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("watermarkdryer", flag.ContinueOnError)
	f.SetOutput(stderr)
	mode := "detect"
	if len(args) > 0 && (args[0] == "detect" || args[0] == "remove") {
		mode = args[0]
		args = args[1:]
	}
	f.StringVar(&mode, "mode", mode, "detect or remove")
	path := f.String("path", "", "file or directory to process (alternative to positional paths)")
	recursive := f.Bool("recursive", true, "scan directories recursively")
	f.BoolVar(recursive, "r", true, "scan directories recursively")
	jsonOutput := f.Bool("json", false, "print a JSON report")
	verbose := f.Bool("verbose", false, "also print clean and skipped files")
	check := f.Bool("check", false, "in detect mode, exit 1 if removal would change any file")
	backup := f.Bool("backup", true, "create .watermarkdryer.bak before changing a file")
	maxSize := f.Int64("max-size", 64<<20, "maximum file size in bytes")
	version := f.Bool("version", false, "print version")
	license := f.Bool("license", false, "print embedded third-party license notices")
	var exclude excludes
	f.Var(&exclude, "exclude", "skip a name or relative path glob; repeatable (Go filepath.Match syntax)")
	var opts watermark.Options
	f.BoolVar(&opts.KeepSpaces, "keep-spaces", false, "report exotic spaces but keep them")
	f.BoolVar(&opts.StripBidi, "strip-bidi", false, "also remove legitimate bidirectional controls")
	f.BoolVar(&opts.StripGlue, "strip-glue", false, "also remove emoji glue and script joiners/selectors")
	f.BoolVar(&opts.AggressiveHomoglyphs, "aggressive-homoglyphs", false, "map selected Cyrillic/fullwidth Latin lookalikes to ASCII")
	f.BoolVar(&opts.NFKC, "nfkc", false, "also apply Unicode NFKC compatibility normalization")
	f.BoolVar(&opts.StripTrailingWhitespace, "strip-trailing-whitespace", false, "remove all trailing spaces/tabs (can affect Markdown breaks and string literals)")
	f.Usage = func() {
		fmt.Fprintln(stderr, "Usage: watermarkdryer [detect|remove] [options] PATH...\n       watermarkdryer --mode detect|remove --path PATH\n\nDetection is the default and never writes files. Directories are recursive by default.\nRemoval edits matching files in place with atomic replacement and backups.\n\nSupported: text/source (UTF-8, BOM-marked UTF-16/32), Markdown YAML metadata,\nHTML meta tags, SVG metadata, PNG/JPEG/WebP provenance metadata.\nFindings are indicators, not proof of AI authorship. Statistical/token and\npixel-domain watermarks and visible image overlays are not supported.\n\nOptions:")
		f.PrintDefaults()
	}
	if err := f.Parse(interspersed(args, f)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *version {
		fmt.Fprintln(stdout, Version)
		return 0
	}
	if *license {
		fmt.Fprintln(stdout, licenses)
		return 0
	}
	if mode != "detect" && mode != "remove" {
		fmt.Fprintln(stderr, "error: --mode must be detect or remove")
		return 2
	}
	if *check && mode != "detect" {
		fmt.Fprintln(stderr, "error: --check is only valid in detect mode")
		return 2
	}
	if *maxSize <= 0 || *maxSize > 1<<40 {
		fmt.Fprintln(stderr, "error: --max-size must be between 1 and 1099511627776")
		return 2
	}
	paths := f.Args()
	if *path != "" {
		paths = append(paths, *path)
	}
	if len(paths) == 0 {
		f.Usage()
		return 2
	}
	rep := report{Version: Version, Mode: mode, Files: []fileResult{}}
	seen := map[string]bool{}
	claudeTextNotChecked := false
	emit := func(fr fileResult) {
		if fr.ClaudeTextWatermark != nil {
			claudeTextNotChecked = true
		}
		if fr.Findings == nil {
			fr.Findings = []watermark.Finding{}
		}
		if *jsonOutput {
			rep.Files = append(rep.Files, fr)
		}
		switch fr.Status {
		case "error":
			rep.Summary.Errors++
		case "skipped":
			rep.Summary.Skipped++
		default:
			rep.Summary.Scanned++
		}
		if len(fr.Findings) > 0 {
			rep.Summary.WithFindings++
		}
		if fr.WouldChange {
			rep.Summary.WouldChange++
		}
		if fr.Changed {
			rep.Summary.Changed++
		}
		if !*jsonOutput {
			printFile(stdout, fr, *verbose)
		}
	}
	process := func(p string, info fs.FileInfo) {
		fr := fileResult{Path: p, Status: "clean"}
		if !info.Mode().IsRegular() {
			fr.Status = "skipped"
			fr.Reason = "symlink or non-regular file"
			emit(fr)
			return
		}
		if info.Size() > *maxSize {
			fr.Status = "skipped"
			fr.Reason = "exceeds --max-size"
			emit(fr)
			return
		}
		data, err := readFile(p, info, *maxSize)
		if err != nil {
			fr.Status = "error"
			fr.Error = err.Error()
			emit(fr)
			return
		}
		out, res, err := watermark.Process(p, data, opts)
		fr.Result = res
		if err != nil {
			fr.Status = "error"
			fr.Error = err.Error()
			emit(fr)
			return
		}
		if res.Unsupported {
			fr.Status = "skipped"
			fr.Reason = "unsupported binary format or text encoding"
			emit(fr)
			return
		}
		if len(res.Findings) > 0 {
			fr.Status = "detected"
		}
		if mode == "remove" && res.WouldChange {
			fr.Backup, err = replaceFile(p, info, data, out, *backup)
			if err != nil {
				fr.Status = "error"
				fr.Error = err.Error()
			} else {
				fr.Status = "removed"
				fr.Changed = true
			}
		}
		emit(fr)
	}
	for _, root := range paths {
		root, err := filepath.Abs(root)
		if err != nil {
			emit(fileResult{Path: root, Status: "error", Error: err.Error()})
			continue
		}
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				emit(fileResult{Path: p, Status: "error", Error: walkErr.Error()})
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			if excluded(p, rel, exclude) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if p != root && !*recursive {
					return filepath.SkipDir
				}
				return nil
			}
			if seen[p] {
				return nil
			}
			seen[p] = true
			info, err := d.Info()
			if err != nil {
				emit(fileResult{Path: p, Status: "error", Error: err.Error()})
				return nil
			}
			process(p, info)
			return nil
		})
		if err != nil {
			emit(fileResult{Path: root, Status: "error", Error: err.Error()})
		}
	}
	if *jsonOutput {
		e := json.NewEncoder(stdout)
		e.SetIndent("", "  ")
		if err := e.Encode(rep); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	} else {
		s := rep.Summary
		fmt.Fprintf(stdout, "Scanned %d files: %d with findings, %d would change, %d changed, %d skipped, %d errors.\n", s.Scanned, s.WithFindings, s.WouldChange, s.Changed, s.Skipped, s.Errors)
		if claudeTextNotChecked {
			fmt.Fprintln(stdout, "NOT CHECKED: "+watermark.ClaudeTextLimitation)
		}
	}
	if rep.Summary.Errors > 0 {
		return 2
	}
	if *check && rep.Summary.WouldChange > 0 {
		return 1
	}
	return 0
}

// Permit flags before or after paths while respecting -- for literal filenames.
func interspersed(args []string, f *flag.FlagSet) []string {
	var flags, paths []string
	for i := 0; i < len(args); i++ {
		s := args[i]
		if s == "--" {
			paths = append(paths, args[i+1:]...)
			break
		}
		if len(s) < 2 || s[0] != '-' {
			paths = append(paths, s)
			continue
		}
		flags = append(flags, s)
		name := strings.TrimLeft(s, "-")
		if strings.Contains(name, "=") {
			continue
		}
		v := f.Lookup(name)
		if v == nil {
			continue
		}
		if b, ok := v.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(append(flags, "--"), paths...)
}

func excluded(p, rel string, patterns []string) bool {
	name := filepath.Base(p)
	if name == ".git" || name == ".hg" || name == ".svn" || strings.HasSuffix(name, backupSuffix) || strings.HasPrefix(name, ".watermarkdryer-tmp-") {
		return true
	}
	for _, pattern := range patterns {
		a, _ := filepath.Match(pattern, name)
		b, _ := filepath.Match(pattern, rel)
		if a || b {
			return true
		}
	}
	return false
}

func printFile(w io.Writer, fr fileResult, verbose bool) {
	if fr.Status == "clean" && !verbose || fr.Status == "skipped" && !verbose {
		return
	}
	label := strings.ToUpper(fr.Status)
	if fr.Status == "clean" {
		label = "NO SUPPORTED INDICATORS"
	}
	fmt.Fprintf(w, "%s %q", label, fr.Path)
	if fr.Error != "" {
		fmt.Fprintf(w, ": %s", fr.Error)
	}
	if fr.Reason != "" {
		fmt.Fprintf(w, ": %s", fr.Reason)
	}
	fmt.Fprintln(w)
	for _, h := range fr.Findings {
		fmt.Fprintf(w, "  %s %s: %s; count=%d; action=%s", h.Kind, h.Codepoint, h.Detail, h.Count, h.Action)
		if h.Verification != "" {
			fmt.Fprintf(w, "; verification=%s", h.Verification)
		}
		if len(h.Locations) > 0 {
			fmt.Fprintf(w, "; first at %d:%d", h.Locations[0].Line, h.Locations[0].Column)
		}
		fmt.Fprintln(w)
	}
	if fr.Backup != "" {
		fmt.Fprintf(w, "  backup: %q\n", fr.Backup)
	}
}

func readFile(path string, expected fs.FileInfo, limit int64) ([]byte, error) {
	l, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !l.Mode().IsRegular() || !os.SameFile(l, expected) {
		return nil, fmt.Errorf("file changed while scanning")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(actual, expected) {
		return nil, fmt.Errorf("file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file grew beyond --max-size")
	}
	return data, nil
}

func replaceFile(path string, info fs.FileInfo, before, after []byte, backup bool) (string, error) {
	// Atomic rename replaces only this directory entry. Refuse hard-linked
	// files because replacing one link would silently leave others uncleaned.
	if hasMultipleLinks(info) {
		return "", fmt.Errorf("refusing to replace a hard-linked file")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".watermarkdryer-tmp-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(after); err == nil {
		err = tmp.Chmod(info.Mode().Perm())
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	current, err := readFile(path, info, int64(len(before))+1)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(current, before) {
		return "", fmt.Errorf("file contents changed during processing")
	}
	backupPath := ""
	if backup {
		backupPath = path + backupSuffix
		b, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return "", fmt.Errorf("cannot create backup (existing backups are never overwritten): %w", err)
		}
		if _, err = b.Write(before); err == nil {
			err = b.Chmod(info.Mode().Perm())
		}
		if err == nil {
			err = b.Sync()
		}
		closeErr = b.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(backupPath)
			return "", err
		}
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return backupPath, err
	}
	return backupPath, nil
}
