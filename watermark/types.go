// Package watermark detects and removes deterministic Unicode carriers and
// provenance metadata. Findings are indicators, not proof of AI authorship.
package watermark

type Options struct {
	KeepSpaces              bool
	StripBidi               bool
	StripGlue               bool
	AggressiveHomoglyphs    bool
	NFKC                    bool
	StripTrailingWhitespace bool
}

type Location struct {
	Offset int `json:"offset"` // zero-based Unicode character offset
	Line   int `json:"line"`   // one-based
	Column int `json:"column"` // one-based Unicode character column
}

type Finding struct {
	Kind         string     `json:"kind"`
	Detail       string     `json:"detail"`
	Codepoint    string     `json:"codepoint,omitempty"`
	Count        int        `json:"count"`
	Action       string     `json:"action"` // remove, replace, or preserve
	Confidence   string     `json:"confidence"`
	Locations    []Location `json:"locations,omitempty"`
	Verification string     `json:"verification,omitempty"`
}

func finding(kind, detail string) Finding {
	f := Finding{Kind: kind, Detail: detail, Count: 1, Action: "remove", Confidence: "indicator"}
	if kind == "provenance_metadata" {
		f.Verification = "not_verified"
	}
	return f
}

type Result struct {
	Format              string                `json:"format"`
	Encoding            string                `json:"encoding,omitempty"`
	Findings            []Finding             `json:"findings"`
	WouldChange         bool                  `json:"would_change"`
	Unsupported         bool                  `json:"unsupported,omitempty"`
	ClaudeTextWatermark *ClaudeTextAssessment `json:"claude_text_watermark,omitempty"`
}
