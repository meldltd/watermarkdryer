# Claude watermark support review

Reviewed on 2026-09-12 against Anthropic's public documentation.

## Text

Claude uses a keyed variant of SynthID-Text that affects token selection. It
does not insert hidden characters. Code has fewer eligible choices, but comments
can carry the signal. Unicode scrubbing therefore cannot establish whether this
watermark is present or remove it. Detection is currently in private preview.
[Anthropic, “How Claude's text watermarking works”](https://www.anthropic.com/news/claude-text-watermark).

The binary remains offline. For decoded text, `claude_text_watermark` reports
`status: "unavailable"`, `detected: null`, and `removal_supported: false`.
These are coverage facts, not findings about the input. Unicode findings, a
successful removal, and `--check` exit code 0 do not change this unknown result.
No guessed API endpoint, tokenizer, secret key, or style-based score is used.

## Files

Anthropic separately attaches signed C2PA provenance to supported generated
files. This identifies processing by Claude only when the credential is properly
verified; a missing credential does not establish human authorship.
[Anthropic Help Center](https://support.claude.com/en/articles/16266773-how-claude-marks-ai-generated-content).

Our existing PNG/JPEG metadata handling covers their documented C2PA containers.
The review found a gap in SVG: C2PA's manifest element is identified by namespace
`http://c2pa.org/manifest`, and the namespace can be declared on the root with an
arbitrary prefix. Detection now resolves XML namespaces instead of requiring
the literal `c2pa` prefix. It also recognizes explicit XMP `CreatorTool` values
while retaining ordinary descriptions mentioning Claude or Anthropic.
[C2PA specification, embedding manifests](https://spec.c2pa.org/specifications/specifications/2.0/specs/C2PA_Specification.html).

Metadata findings now say `verification: "not_verified"`. The binary detects
metadata indicators and removes matching containers; it does not authenticate
their signatures, issuer, or claims. No synthetic test fixture is represented
as a real Claude-issued credential. External manifests and unsupported formats
remain outside the scanner's coverage.
