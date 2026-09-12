package watermark

// These facts describe detector coverage, not evidence about any input file.
// Anthropic's public description (reviewed 2026-09-12) specifies keyed
// SynthID-Text sampling, no hidden characters, and a private-preview detector.
const ClaudeWatermarkSource = "https://www.anthropic.com/news/claude-text-watermark"

const ClaudeTextLimitation = "Claude's keyed SynthID-Text watermark was not checked. Unicode/metadata cleanup does not detect or remove it; Anthropic's detector is in private preview."

type ClaudeTextAssessment struct {
	Status           string `json:"status"`
	Detected         *bool  `json:"detected"` // null means unknown, never false.
	Method           string `json:"method"`
	RemovalSupported bool   `json:"removal_supported"`
	Reason           string `json:"reason"`
	Source           string `json:"source"`
}

func unavailableClaudeAssessment() *ClaudeTextAssessment {
	return &ClaudeTextAssessment{
		Status: "unavailable",
		Method: "keyed_synthid_text",
		Reason: ClaudeTextLimitation,
		Source: ClaudeWatermarkSource,
	}
}
