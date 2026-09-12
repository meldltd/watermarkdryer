# watermarkdryer

A standalone Go CLI that detects and removes deterministic watermark carriers
from files. Give it a **filesystem path**, either a file or a directory.
Detection prints findings. Removal edits files in place. Directories are scanned
recursively by default; multiple input paths are supported.

The implementation prioritizes **text and source files**, following the Unicode
cleaning rules in [watermarks-remover](https://github.com/guillaumemeyer/watermarks-remover)
at commit `81d808d5d71bb22a02b1bdc3df293a2d93422778`.
It also supports selected document and image provenance metadata.

See the [techniques review](docs/techniques.md) for coverage and limitations of
Unicode, C2PA text wrappers, whitespace carriers, and statistical/media marks.

**Claude's actual text watermark is not detectable by this offline scanner.**
The [Claude support review](docs/claude-watermarks.md) explains the public
documentation, the explicit unknown result, and supported C2PA metadata handling.

## Build and run

Building requires Go 1.26 or later. The compiled binary needs no Go installation,
Python, external commands, model, API key, or network connection.

```sh
make build

# Read-only detection of a project directory
./bin/watermarkdryer detect /path/to/project

# Remove detected carriers in place, with a backup for each changed file
./bin/watermarkdryer remove /path/to/project

# A single file; detect is also the default mode
./bin/watermarkdryer /path/to/file.go

# Equivalent flag interface
./bin/watermarkdryer --mode remove --path /path/to/project

# Machine-readable report and a CI failure if files would change
./bin/watermarkdryer detect /path/to/project --json --check

# Limit traversal or exclude names/paths
./bin/watermarkdryer detect /path/to/project --recursive=false
./bin/watermarkdryer detect /path/to/project --exclude node_modules --exclude '*.min.js'
```

Flags may appear before or after paths. Use `--` before a filename beginning with
`-`. Exclusion globs follow Go's `filepath.Match` rules: `*` does not cross a path
separator, and `**` has no special recursive meaning. A matching directory name
excludes its entire subtree. `.git`, `.hg`, `.svn`, this tool's backups, and its
temporary files are always excluded. Other hidden files are scanned.

## Detection and removal

Example output:

```text
DETECTED "/path/to/project/main.go"
  zero_width U+200B: ZERO WIDTH SPACE; count=1; action=remove; first at 2:6
Scanned 1 files: 1 with findings, 1 would change, 0 changed, 0 skipped, 0 errors.
NOT CHECKED: Claude's keyed SynthID-Text watermark was not checked. Unicode/metadata cleanup does not detect or remove it; Anthropic's detector is in private preview.
```

Each finding includes a category, character name/codepoint where applicable,
count, and proposed action. JSON includes up to ten sample positions per
character and distinguishes `would_change` from files actually `changed`.
Positions are one-based lines/Unicode character columns and zero-based Unicode
character offsets in the decoded original text; an initial encoding BOM is not
counted. Line numbers advance on LF, including CRLF. Image metadata findings
identify the segment or chunk rather than a text position.

For decoded text, JSON also includes `claude_text_watermark` with
`status: "unavailable"`, `detected: null`, and `removal_supported: false`.
This applies even after removal or when no local indicators were found. A
metadata finding's `verification: "not_verified"` means neither its signature
nor its issuer has been authenticated. These indicators never establish that
Claude produced a file. Verbose output labels clean files `NO SUPPORTED INDICATORS`;
the JSON `status: "clean"` remains a statement about supported local checks only.

Detection and removal use the same cleaning pipeline. `action=preserve` reports
a character that the selected options retain. Findings are **indicators**, not
proof of a watermark or AI authorship; ordinary typography and multilingual
writing can contain the same characters. Clean files are silent unless
`--verbose` is supplied. Skipped-file reasons are available with `--verbose` or
`--json`; the summary always reports skipped and failed files.

| Input | Detection/removal coverage |
| --- | --- |
| Text and source files | Zero-width characters, soft hyphens, invisible format controls, detached variation selectors and tag characters, private-use characters, noncharacters, reserved ignorables, and unusual spaces |
| Markdown/MDX | Text rules plus AI/provenance fields in a leading YAML block mapping; ordinary prose and fenced examples are retained |
| HTML | Text rules plus matching real `<meta>` tags; tag lookalikes inside script strings are retained |
| SVG | Text rules plus matching metadata/RDF/XMP elements, namespace-resolved C2PA manifests, and explicit XMP creator tools; drawing elements are retained |
| PNG | C2PA/JUMBF `caBX`, matching `tEXt`/`zTXt`/`iTXt` fields, and matching EXIF chunks |
| JPEG | JUMBF APP11 fragments, matching APP1/APP13 metadata, and explicit provenance comments, including metadata between scans |
| WebP | C2PA/JUMBF chunks and matching XMP/EXIF chunks; RIFF lengths and VP8X metadata flags are updated |

UTF-8 is supported with or without a BOM. UTF-16LE/BE and UTF-32LE/BE require a
BOM. Encoding, BOM, CRLF/LF line endings, ordinary whitespace, and unchanged
text are preserved. A BOM inside the text body is treated as a carrier.
Extensionless UTF text is supported. Known binary signatures take precedence
over a misleading text extension. Invalid supported text or malformed recognized
images produce an error, and that file is not changed.

The default text rules preserve emoji ZWJ sequences, presentation selectors,
complete subdivision-flag tag sequences, script joiners, CJK variation
selectors, same-script Mongolian/Khmer/Hangul controls, legitimate directional
marks and paired embeddings, and orthographic script controls. Directional
overrides and unmatched embeddings are removed. Detached selectors whose base
is removed are also removed in the same pass.

Optional text settings apply in both modes:

| Flag | Effect |
| --- | --- |
| `--keep-spaces` | Report unusual spaces without converting them to ASCII spaces |
| `--strip-bidi` | Also remove normally preserved directional marks/embeddings |
| `--strip-glue` | Also remove normally preserved emoji/script glue and selectors |
| `--aggressive-homoglyphs` | Map selected Cyrillic and fullwidth Latin lookalikes to ASCII |
| `--nfkc` | Apply compatibility normalization, such as ligatures/fullwidth characters |
| `--strip-trailing-whitespace` | Remove all trailing ASCII spaces/tabs; can affect Markdown hard breaks and multiline strings |

Text cleaning acts on actual decoded characters everywhere in the file,
including strings and comments in source code. It does not interpret literal
`\u200b` escape sequences, HTML character references, or language syntax.
Changes to spaces, private-use characters, or the opt-in transformations can
affect intentional typography, string values, or identifiers; inspect findings
and use the preserved original when reviewing such changes.

C2PA text wrappers receive an additional `c2pa_text_manifest` finding after
framing validation. Their individual Unicode characters may also be reported.
Malformed recognized wrappers prevent rewriting that file. Signatures and
content hashes remain unverified.

Long SNOW-compatible trailing space/tab patterns are reported as heuristic
`whitespace_carrier` findings with `action: "preserve"` by default. Enable
`--strip-trailing-whitespace` in either mode to inspect/apply removal of all
trailing spaces/tabs. Ordinary trailing whitespace is retained without that flag.

## File handling

- Removal creates `filename.watermarkdryer.bak` containing the original bytes,
  then replaces the file using a synchronized sibling temporary file and rename.
  Ordinary permission bits are preserved. An existing backup is never overwritten;
  the affected file returns an error instead. `--backup=false` explicitly disables
  backup creation.
- Already-clean files retain their bytes and modification time, including when a
  backup from a previous run exists. Detection never creates backups or writes files.
- Symlinks, devices, and other nonregular files are skipped. On Unix, removal
  refuses files with multiple hard links so only one link cannot be silently changed.
- Files larger than `--max-size` (64 MiB by default) are skipped. Decompressed PNG
  text metadata is limited to 16 MiB. Traversal and results are ordered by path.
- Failures are reported per file; other files continue. Replacement is atomic per
  file, **not a transaction across a directory**. Changed contents detected before
  replacement cause an error. Do not edit the same file concurrently with removal;
  this is not an exclusive filesystem lock.
- Replacing an inode does not preserve extended attributes, ACLs, owner identity,
  or creation time. Image pixels are not decoded/re-encoded, but removing an entire
  matching metadata segment can also remove unrelated properties in that segment
  (such as orientation or other EXIF fields).

Exit codes:

| Code | Meaning |
| --- | --- |
| `0` | Processing succeeded; detection may have findings |
| `1` | `detect --check`: at least one file would change |
| `2` | Invalid arguments, input/parse failure, or a failed write; takes precedence over `1` |

Unsupported or oversized files are counted as skipped, not reported as clean.
Review the skipped count when using `--check` to audit a whole directory.
`--check` only gates supported local cleanup; it cannot certify absence of
Claude's statistical watermark.

## Scope

This is a deterministic Go implementation, **not full feature parity** with the
reference's service and optional model backends. It cannot detect or remove
statistical/token-sampling watermarks, pixel-domain SynthID, visible logos,
or arbitrary steganography. A clean report means no supported indicators were
found with the selected options.

PDF, Office/EPUB/ODT/ZIP containers, audio/video, GIF, TIFF, BMP, AVIF, and HEIC are
currently skipped. It does not parse a git/unified diff patch: its input is a
filesystem path. Markdown metadata editing covers top-level YAML block mappings,
not flow mappings, nested generic metadata schemes, or embedded files. It refuses
an edit that would break retained YAML aliases. Image metadata scanning does not
validate cryptographic C2PA signatures or search encoded pixels.

## Development

```sh
make test
make check
go test ./watermark -run '^$' -fuzz FuzzProcess -fuzztime=20s
make release
```

`make release` builds macOS (arm64/amd64), Linux (arm64/amd64), and Windows
(amd64) binaries. Tests cover contextual Unicode handling, original positions,
encoding and newline preservation, metadata false positives, lossless PNG/JPEG
segment removal, WebP chunk preservation, malformed inputs, read-only detection,
recursive traversal, backups, symlinks, hard links, and repeated removal.

The Unicode port was compared with 23,300 generated reference cases. Forty-four
cases expose an upstream detached-Mongolian-selector behavior that requires
another cleaning pass; this implementation removes those remnants in one pass.
All compared outputs match the reference after its output stabilizes.

See [LICENSE](LICENSE) and [third-party notices](internal/cli/licenses.txt).
The notices are also embedded in every binary: `watermarkdryer --license`.
