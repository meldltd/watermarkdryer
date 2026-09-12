# Watermarking and hidden-content techniques

Review date: 2026-09-12. Detection of an encoding or metadata container does not
establish authorship. Watermarks and provenance records are related but distinct.

| Family | Status in this binary |
| --- | --- |
| Zero-width characters, Unicode tags, detached variation selectors, unusual spaces | Existing contextual Unicode rules |
| Confusable substitutions and compatibility variants | Existing opt-in `--aggressive-homoglyphs` and `--nfkc`; cannot determine why a lookalike was used |
| C2PA text wrappers | New framing detection and Unicode removal, with a dedicated `c2pa_text_manifest` finding |
| Trailing-space/tab encodings | New SNOW-compatible heuristic, preserved by default; optional trailing-whitespace removal |
| C2PA and generator metadata | Existing PNG/JPEG/WebP/SVG support, extended for namespace-qualified XMP external-manifest references |
| Statistical token choices: SynthID-Text, green-list schemes, keyed sampling | Unsupported without matching configuration, tokenizer, keys and/or model; no guessed scores |
| Image/audio signals, spread-spectrum, transform-domain or learned marks | Unsupported; metadata removal does not remove these signals |
| Linguistic, semantic, font/layout, or executable-code transformations | No general reliable detector implemented; rewriting can alter meaning or behavior |

## C2PA embedded in plain text

C2PA 2.3 Appendix A.7 specifies a `C2PATXT` wrapper using a FEFF delimiter and
variation selectors to represent bytes. Detection checks the version, declared
payload length, resource limit and outer JUMBF framing. It does not verify
signatures, assertions, issuer or content hash. Malformed recognized wrappers
produce an error and prevent replacement. An initial encoding BOM is retained.
Multiple wrappers are reported independently.
[C2PA specification](https://spec.c2pa.org/specifications/specifications/2.3/specs/C2PA_Specification).

Wrapper findings complement individual Unicode findings; their counts should
not be summed as independent watermark occurrences. Legitimate script/emoji
selectors retain their existing preservation rules. Variation selectors have
legitimate Unicode uses, so their presence alone does not prove a hidden payload.
[Unicode character semantics](https://www.unicode.org/versions/Unicode17.0.0/core-spec/chapter-23/).

## Trailing whitespace

SNOW hides bits using groups of spaces separated by tabs at line ends. Natural
whitespace can have the same form.
[SNOW's technical description](https://darkside.com.au/snow/description.html).

The default heuristic reports a trailing run starting with a tab, containing at
least 16 characters and four tabs, with no group longer than seven spaces. It
can miss short/distributed messages and flag ordinary formatting. It does not
decode, decrypt, authenticate or prove a SNOW message.

```sh
./bin/watermarkdryer detect /path --strip-trailing-whitespace
./bin/watermarkdryer remove /path --strip-trailing-whitespace
```

The flag removes **all** trailing ASCII spaces/tabs, including ordinary ones.
Line endings and non-trailing indentation are retained. Markdown hard breaks,
multiline string values and whitespace-sensitive languages may change, so this
transformation is opt-in. Without the flag, heuristic findings have
`action: "preserve"` and do not cause `--check` to fail.

## External metadata references

C2PA permits XMP's Dublin Core Terms `provenance` property to point to an external
manifest. The detector resolves the property by namespace URI, even when the
reference contains no recognizable vendor/format name.
[C2PA manifest discovery](https://spec.c2pa.org/specifications/specifications/2.3/specs/C2PA_Specification#_by_reference_or_uri).

This works within supported image metadata. Matching chunks/segments are removed
as before. URLs are never fetched; remote or sidecar manifests are neither
verified nor deleted. Standalone XMP, archive manifests and HTTP headers remain
outside metadata coverage.

## Why no generic statistical detector

The public SynthID implementation requires watermark-specific keys/configuration
and a matching detector. An arbitrary key or AI-style classifier cannot verify
somebody else's watermark.
[DeepMind/Hugging Face implementation](https://huggingface.co/blog/synthid-text).

The binary does not paraphrase code, change pixels or alter audio to defeat an
unmeasured signal. Zero local findings do not establish that statistical,
semantic or media watermarks are absent.
