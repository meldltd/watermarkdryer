package watermark

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

// Process is read-only: it returns cleaned bytes; the caller chooses whether
// to write them. Malformed recognized formats return an error, never a rewrite.
func Process(name string, data []byte, opts Options) ([]byte, Result, error) {
	r := Result{Findings: []Finding{}}
	ext := strings.ToLower(filepath.Ext(name))
	var out []byte
	var hits []Finding
	var err error
	switch {
	case bytes.HasPrefix(data, pngSignature):
		r.Format = "png"
		out, hits, err = cleanPNG(data)
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8}):
		r.Format = "jpeg"
		out, hits, err = cleanJPEG(data)
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		r.Format = "webp"
		out, hits, err = cleanWebP(data)
	default:
		if expectedImage(ext) {
			return nil, r, fmt.Errorf("invalid or unsupported %s image", ext)
		}
		if knownBinary(data, ext) {
			r.Format = "unsupported"
			r.Unsupported = true
			return data, r, nil
		}
		text, encoding, decodeErr := decodeText(data)
		if decodeErr != nil {
			if textExtension(ext) {
				return nil, r, decodeErr
			}
			r.Format = "unsupported"
			r.Unsupported = true
			return data, r, nil
		}
		r.Format = "text"
		r.Encoding = encoding.name
		r.ClaudeTextWatermark = unavailableClaudeAssessment()
		manifestHits, manifestErr := inspectTextManifests(text, len(encoding.bom) > 0)
		if manifestErr != nil {
			return nil, r, manifestErr
		}
		// Compute both kinds of findings against original input locations.
		unicodeCleaned, unicodeHits := CleanText(text, opts)
		cleaned := text
		switch ext {
		case ".md", ".markdown", ".mdx":
			r.Format = "markdown"
			cleaned, hits, err = cleanMarkdown(text)
		case ".html", ".htm":
			r.Format = "html"
			cleaned, hits, err = cleanHTML(text)
		case ".svg":
			r.Format = "svg"
			cleaned, hits, err = cleanSVG(text)
		}
		if err != nil {
			return nil, r, err
		}
		if cleaned == text {
			cleaned = unicodeCleaned
		} else {
			cleaned, _ = CleanText(cleaned, opts)
		}
		hits = append(hits, unicodeHits...)
		hits = append(hits, manifestHits...)
		out = encoding.encode(cleaned)
	}
	if err != nil {
		return nil, r, err
	}
	r.Findings = append(r.Findings, hits...)
	r.WouldChange = !bytes.Equal(data, out)
	return out, r, nil
}

func expectedImage(ext string) bool {
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp"
}

func textExtension(ext string) bool {
	return strings.Contains("|.txt|.text|.md|.markdown|.mdx|.html|.htm|.svg|.xml|.go|.py|.rs|.js|.jsx|.mjs|.cjs|.ts|.tsx|.css|.scss|.json|.yaml|.yml|.toml|.csv|.tsv|.c|.h|.cpp|.cc|.hpp|.cs|.java|.kt|.kts|.swift|.scala|.dart|.rb|.php|.lua|.pl|.r|.sh|.bash|.zsh|.ps1|.sql|.vue|.svelte|.astro|.rst|.adoc|.org|.po|.pot|.strings|.arb|.resx|.properties|.ini|.cfg|.conf|.tex|.ltx|.log|", "|"+ext+"|")
}

func knownBinary(data []byte, ext string) bool {
	for _, magic := range [][]byte{[]byte("%PDF-"), []byte("PK\x03\x04"), []byte("PK\x05\x06"), []byte("GIF87a"), []byte("GIF89a"), []byte("\x7fELF"), []byte("\x1f\x8b"), []byte("SQLite format 3"), []byte("RIFF"), []byte("fLaC"), []byte("ID3")} {
		if bytes.HasPrefix(data, magic) {
			return true
		}
	}
	return strings.Contains("|.pdf|.doc|.docx|.xlsx|.xls|.ppt|.pptx|.odt|.epub|.zip|.gz|.tar|.7z|.rar|.gif|.avif|.heic|.heif|.tif|.tiff|.bmp|.mp3|.wav|.mp4|.mov|.m4a|.flac|.exe|.dll|.so|.dylib|.a|.o|.wasm|.woff|.woff2|.ttf|.ico|.bin|", "|"+ext+"|")
}
