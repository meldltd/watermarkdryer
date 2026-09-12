package watermark

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeTextIsUnknownRegardlessOfUnicode(t *testing.T) {
	for _, input := range []string{"The weather today was cold and overcast.", "// Claude generated this function.\nfunc add(a, b int) int { return a + b }\n", "Invisible\u200b character and narrow\u202fspace."} {
		out, r, err := Process("source.go", []byte(input), Options{})
		if err != nil {
			t.Fatal(err)
		}
		a := r.ClaudeTextWatermark
		if a == nil || a.Status != "unavailable" || a.Detected != nil || a.RemovalSupported || a.Method != "keyed_synthid_text" {
			t.Fatalf("fabricated a Claude detection: %+v", a)
		}
		encoded, err := json.Marshal(r)
		if err != nil || !bytes.Contains(encoded, []byte(`"detected":null`)) {
			t.Fatalf("unknown result not represented as null: %s %v", encoded, err)
		}
		_, after, err := Process("source.go", out, Options{})
		if err != nil || after.ClaudeTextWatermark.Detected != nil || after.ClaudeTextWatermark.Status != "unavailable" {
			t.Fatal("cleanup incorrectly verified Claude watermark removal")
		}
		if !strings.ContainsRune(input, '\u200b') && (r.WouldChange || len(r.Findings) != 0) {
			t.Fatal("plain words incorrectly treated as watermark evidence")
		}
	}
}

func TestSVGManifestNamespaceAliases(t *testing.T) {
	for _, prefix := range []string{"c2pa", "proof"} {
		input := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:` + prefix + `="http://c2pa.org/manifest"><metadata><` + prefix + `:manifest>AAAAAA==</` + prefix + `:manifest></metadata><text>Claude Monet</text></svg>`
		out, r, err := Process("drawing.svg", []byte(input), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !r.WouldChange || len(r.Findings) != 1 || bytes.Contains(out, []byte("AAAAAA==")) || !bytes.Contains(out, []byte("Claude Monet")) {
			t.Fatalf("missed aliased manifest: %s %+v", out, r)
		}
		if r.Findings[0].Verification != "not_verified" {
			t.Fatal("must not imply a validated Claude signature")
		}
		_, after, err := Process("drawing.svg", out, Options{})
		if err != nil || after.WouldChange || len(after.Findings) != 0 {
			t.Fatal("not idempotent")
		}
	}
}

func TestSVGDescriptionsAndCreatorTool(t *testing.T) {
	for _, input := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><metadata><description>A painting by Claude Monet</description></metadata></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><metadata><description>A guide to Anthropic and Claude</description></metadata></svg>`,
		`<svg xmlns:proof="https://example.org/not-a-manifest"><metadata><proof:manifest>AAAAAA==</proof:manifest></metadata></svg>`,
	} {
		out, r, err := Process("drawing.svg", []byte(input), Options{})
		if err != nil || r.WouldChange || len(r.Findings) != 0 || string(out) != input {
			t.Fatalf("false metadata match: %+v %v", r, err)
		}
	}
	for _, body := range []string{`<metadata><x:CreatorTool>Claude</x:CreatorTool></metadata>`, `<metadata><x:Description x:CreatorTool="Claude"/></metadata>`} {
		input := `<svg xmlns:x="http://ns.adobe.com/xap/1.0/">` + body + `<text>Keep</text></svg>`
		out, r, err := Process("drawing.svg", []byte(input), Options{})
		if err != nil || !r.WouldChange || len(r.Findings) != 1 || !bytes.Contains(out, []byte("<text>Keep</text>")) {
			t.Fatalf("missed explicit generator: %+v %v", r, err)
		}
	}
}
