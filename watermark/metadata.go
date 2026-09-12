package watermark

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
	"golang.org/x/net/html"
)

var provenanceMarker = regexp.MustCompile(`(?i)\b(?:c2pa|c2ma|content[-_ ]?credentials?|contentauth|synthid|aigc|(?:trained[-_ ]?)?algorithmic[-_ ]?media|digital[-_ ]?source[-_ ]?type)\b|\bcai:`)
var products = regexp.MustCompile(`(?i)\b(?:claude|anthropic|openai|chatgpt|dall[- ]?e|midjourney|stable diffusion|sdxl|flux|dreamstudio|leonardo[ .]ai|novelai|ideogram|gemini|imagen|grok|sora|veo|kling|runway|firefly|synthid)\b`)
var aiStatement = regexp.MustCompile(`(?i)\b(?:ai[- ]generated|generated (?:by|with) (?:ai\b|claude\b|chatgpt\b|openai\b|gemini\b|midjourney\b|stable diffusion\b))`)

func normalizeKey(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToLower(s))
}

func metadataHit(key, value string) bool {
	k := normalizeKey(key)
	switch k {
	case "ai", "aigenerated", "aigc", "c2pa", "contentcredentials", "synthid", "provenance", "digitalsourcetype", "openai", "anthropic", "claude":
		return true
	case "generator", "generatedby", "createdwith", "creator", "creatortool", "producer", "software", "tool", "engine", "model", "llm", "parameters":
		return products.MatchString(value) || provenanceMarker.MatchString(value) || aiStatement.MatchString(value)
	}
	return provenanceMarker.MatchString(value) || aiStatement.MatchString(value)
}

type edit struct {
	start, end  int
	replacement string
}

func applyEdits(text string, edits []edit) string {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out strings.Builder
	pos := 0
	for _, e := range edits {
		if e.start < pos {
			continue
		}
		out.WriteString(text[pos:e.start])
		out.WriteString(e.replacement)
		pos = e.end
	}
	out.WriteString(text[pos:])
	return out.String()
}

func at(text string, offset int) Location {
	l := Location{Line: 1, Column: 1}
	for _, r := range text[:offset] {
		l.Offset++
		if r == '\n' {
			l.Line++
			l.Column = 1
		} else {
			l.Column++
		}
	}
	return l
}

// Markdown is edited only in a leading YAML frontmatter block. YAML node
// locations let us preserve unrelated formatting and avoid matching code fences.
func cleanMarkdown(text string) (string, []Finding, error) {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return text, nil, nil
	}
	end := 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "---" && strings.TrimSpace(lines[end]) != "..." {
		end++
	}
	if end == len(lines) {
		return text, nil, nil
	} // Markdown horizontal rule, not frontmatter.
	body := strings.Join(lines[1:end], "")
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return "", nil, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return text, nil, nil
	}
	root := doc.Content[0]
	var edits []edit
	var findings []Finding
	starts := make([]int, end+1)
	for i := 1; i <= end; i++ {
		starts[i] = starts[i-1] + len(lines[i-1])
	}
	for i := 0; i < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		// Complex and flow mappings are left intact rather than deleting a
		// neighbouring field sharing the same source line.
		if key.Kind != yaml.ScalarNode || root.Style&yaml.FlowStyle != 0 {
			continue
		}
		var val strings.Builder
		var visit func(*yaml.Node)
		visit = func(n *yaml.Node) {
			val.WriteString(n.Value)
			val.WriteByte(' ')
			for _, c := range n.Content {
				visit(c)
			}
		}
		visit(value)
		if !metadataHit(key.Value, val.String()) {
			continue
		}
		last := end
		if i+2 < len(root.Content) {
			last = root.Content[i+2].Line
		}
		start := starts[key.Line]
		stop := starts[last]
		edits = append(edits, edit{start, stop, ""})
		f := finding("provenance_metadata", "YAML field "+key.Value)
		f.Locations = []Location{at(text, start)}
		findings = append(findings, f)
	}
	cleaned := applyEdits(text, edits)
	if len(edits) > 0 {
		// A removed provenance field may define an anchor used by a retained
		// field. Refuse the edit instead of writing invalid frontmatter.
		cleanLines := strings.SplitAfter(cleaned, "\n")
		last := 1
		for last < len(cleanLines) && strings.TrimSpace(cleanLines[last]) != "---" && strings.TrimSpace(cleanLines[last]) != "..." {
			last++
		}
		var checked yaml.Node
		if err := yaml.Unmarshal([]byte(strings.Join(cleanLines[1:last], "")), &checked); err != nil {
			return "", nil, fmt.Errorf("removing provenance would break YAML references: %w", err)
		}
	}
	return cleaned, findings, nil
}

func cleanHTML(text string) (string, []Finding, error) {
	z := html.NewTokenizer(strings.NewReader(text))
	var edits []edit
	var findings []Finding
	pos := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				return "", nil, z.Err()
			}
			break
		}
		raw := string(z.Raw())
		start := pos
		pos += len(raw)
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		t := z.Token()
		if t.Data != "meta" {
			continue
		}
		key, value := "", ""
		for _, a := range t.Attr {
			switch a.Key {
			case "name", "property", "http-equiv":
				key = a.Val
			case "content":
				value = a.Val
			}
		}
		if !metadataHit(key, value) {
			continue
		}
		edits = append(edits, edit{start, pos, ""})
		f := finding("provenance_metadata", "HTML meta "+key)
		f.Locations = []Location{at(text, start)}
		findings = append(findings, f)
	}
	return applyEdits(text, edits), findings, nil
}

func cleanSVG(text string) (string, []Finding, error) {
	d := xml.NewDecoder(strings.NewReader(text))
	var edits []edit
	var findings []Finding
	depth, start, targetDepth := 0, 0, 0
	targetMatched := false
	var elements []xml.Name
	for {
		before := int(d.InputOffset())
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("invalid SVG XML: %w", err)
		}
		switch t := t.(type) {
		case xml.StartElement:
			depth++
			elements = append(elements, t.Name)
			if targetDepth == 0 && (t.Name.Local == "metadata" || t.Name.Local == "RDF" || t.Name.Local == "xmpmeta") {
				start, targetDepth = before, depth
				targetMatched = false
			}
			if targetDepth > 0 {
				// C2PA specifies the namespace URI, not a mandatory XML prefix.
				// The namespace may be declared on <svg>, outside this block.
				if t.Name.Space == "http://c2pa.org/manifest" && t.Name.Local == "manifest" {
					targetMatched = true
				}
				for _, a := range t.Attr {
					if a.Name.Space != "xmlns" && a.Name.Local != "xmlns" && metadataHit(a.Name.Local, a.Value) {
						targetMatched = true
					}
				}
			}
		case xml.CharData:
			if targetDepth > 0 && len(elements) > 0 && metadataHit(elements[len(elements)-1].Local, string(t)) {
				targetMatched = true
			}
		case xml.EndElement:
			if targetDepth == depth {
				stop := int(d.InputOffset())
				block := text[start:stop]
				if targetMatched || provenanceMarker.MatchString(block) || aiStatement.MatchString(block) {
					edits = append(edits, edit{start, stop, ""})
					f := finding("provenance_metadata", "SVG metadata element")
					f.Locations = []Location{at(text, start)}
					findings = append(findings, f)
				}
				targetDepth = 0
			}
			depth--
			elements = elements[:len(elements)-1]
		}
	}
	return applyEdits(text, edits), findings, nil
}

func metadataBytes(data []byte) bool {
	// EXIF commonly stores UTF-16 text. This is only applied within a parsed
	// metadata segment, never to image pixels or arbitrary binary payloads.
	s := string(bytes.ReplaceAll(data, []byte{0}, nil))
	return provenanceMarker.MatchString(s) || aiStatement.MatchString(s) || hasXMPProvenance([]byte(s))
}
