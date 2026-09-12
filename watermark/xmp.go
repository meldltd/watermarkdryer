package watermark

import (
	"bytes"
	"encoding/xml"
)

// A remote manifest URI need not include the string "c2pa". XMP discovery
// uses the expanded Dublin Core Terms property name, including alias prefixes.
func hasXMPProvenance(data []byte) bool {
	start := bytes.IndexByte(data, '<')
	if start < 0 {
		return false
	}
	d := xml.NewDecoder(bytes.NewReader(data[start:]))
	for {
		t, err := d.Token()
		if err != nil {
			return false
		}
		e, ok := t.(xml.StartElement)
		if !ok {
			continue
		}
		if e.Name.Space == "http://purl.org/dc/terms/" && e.Name.Local == "provenance" {
			return true
		}
		for _, a := range e.Attr {
			if a.Name.Space == "http://purl.org/dc/terms/" && a.Name.Local == "provenance" {
				return true
			}
		}
	}
}
