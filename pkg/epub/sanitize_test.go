package epub

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestSanitizeAndWrapXHTML_Fragment(t *testing.T) {
	raw := `<div id="sbo-rt-content"><section data-type="chapter" epub:type="chapter" data-pdf-bookmark="Chapter 6. SRE"><div class="chapter">
<h1>Chapter 6</h1>
<p>Hello world & test &nbsp; &mdash; &ndash; image below:</p>
<img src="/api/v2/epubs/urn:orm:book:123/files/assets/fig1.png" width="100">
<br>
<script src="/TCcAz20WUeaSQAm0l8-6/track.js"></script>
<div id="sec-overlay" style="display:none;"><div id="sec-container"></div></div>
</div></section></div>`

	cleaned := SanitizeAndWrapXHTML(raw, "ch06.html", "epub.css")

	// Verify required EPUB namespaces
	if !strings.Contains(cleaned, `xmlns="http://www.w3.org/1999/xhtml"`) {
		t.Errorf("Missing XHTML namespace: %s", cleaned)
	}
	if !strings.Contains(cleaned, `xmlns:epub="http://www.idpf.org/2007/ops"`) {
		t.Errorf("Missing epub namespace: %s", cleaned)
	}

	// Verify scripts and bot overlays removed
	if strings.Contains(cleaned, "TCcAz") || strings.Contains(cleaned, "sec-overlay") {
		t.Errorf("Bot artifacts not removed: %s", cleaned)
	}

	// Verify image url rewritten
	if strings.Contains(cleaned, "/api/v2/epubs") {
		t.Errorf("API URL not rewritten: %s", cleaned)
	}
	if !strings.Contains(cleaned, `src="assets/fig1.png"`) {
		t.Errorf("Expected src=\"assets/fig1.png\", got: %s", cleaned)
	}

	// Verify void tags self-closed
	if !strings.Contains(cleaned, `/>`) {
		t.Errorf("Expected self-closed tags: %s", cleaned)
	}

	// Verify valid XML
	var dummy struct{}
	if err := xml.Unmarshal([]byte(cleaned), &dummy); err != nil {
		t.Fatalf("Cleaned XHTML is not valid XML: %v\nContent:\n%s", err, cleaned)
	}
}

func TestSanitizeAndWrapXHTML_AlreadyFullDoc(t *testing.T) {
	raw := `<!DOCTYPE html><html><head><title>Test</title></head><body><section epub:type="chapter"><h1>Test</h1><img src="pic.jpg"></section></body></html>`

	cleaned := SanitizeAndWrapXHTML(raw, "test.html", "")

	if !strings.Contains(cleaned, `xmlns:epub="http://www.idpf.org/2007/ops"`) {
		t.Errorf("Missing epub namespace on full document")
	}

	var dummy struct{}
	if err := xml.Unmarshal([]byte(cleaned), &dummy); err != nil {
		t.Fatalf("Cleaned full doc is not valid XML: %v", err)
	}
}

func TestSanitizeAndWrapXHTML_ExtractSBOContent(t *testing.T) {
	raw := `<!DOCTYPE html>
<html>
<head><title>Web Page</title><script src="analytics.js"></script></head>
<body>
<header class="sbo-header"><nav><div>Navigation Menu</div></nav></header>
<div class="controls"><a href="prev.html">Prev</a><a href="next.html">Next</a></div>
<div id="sbo-rt-content">
  <div class="chapter" id="ch1">
    <h1>Chapter 1: The Core</h1>
    <p>This is the real book text with nested <div><span>cards</span></div>.</p>
  </div>
</div>
<footer class="sbo-footer"><p>Copyright 2026 O'Reilly Media</p></footer>
</body>
</html>`

	cleaned := SanitizeAndWrapXHTML(raw, "ch01.xhtml", "Styles/default.css")

	// Outer web header, controls, and footer MUST be stripped
	if strings.Contains(cleaned, "sbo-header") || strings.Contains(cleaned, "Navigation Menu") {
		t.Errorf("Outer web header was not stripped: %s", cleaned)
	}
	if strings.Contains(cleaned, "sbo-footer") || strings.Contains(cleaned, "Copyright 2026 O'Reilly") {
		t.Errorf("Outer web footer was not stripped: %s", cleaned)
	}
	if strings.Contains(cleaned, "analytics.js") {
		t.Errorf("Scripts not stripped: %s", cleaned)
	}

	// Real book content must be preserved
	if !strings.Contains(cleaned, "Chapter 1: The Core") {
		t.Errorf("Book title not preserved: %s", cleaned)
	}
	if !strings.Contains(cleaned, "This is the real book text with nested") {
		t.Errorf("Book body text not preserved: %s", cleaned)
	}

	// Stylesheet link and inline reset must be present
	if !strings.Contains(cleaned, `<link rel="stylesheet" type="text/css" href="Styles/default.css" />`) {
		t.Errorf("Default stylesheet link missing: %s", cleaned)
	}
	if !strings.Contains(cleaned, "#sbo-rt-content *") {
		t.Errorf("Inline reset CSS missing: %s", cleaned)
	}

	var dummy struct{}
	if err := xml.Unmarshal([]byte(cleaned), &dummy); err != nil {
		t.Fatalf("Cleaned XHTML with extracted sbo-rt-content is not valid XML: %v", err)
	}
}


func TestSanitizeAndWrapXHTML_AdvancedEntitiesAndXMLValidity(t *testing.T) {
	raw := `<div class="content">
  <p>Accents: Caf&eacute; &uuml;ber &ntilde;o &amp; symbols: &thinsp; &ensp; &dagger; &plusmn; &infin;</p>
  <p>Unknown entity: &foobarunknown; and stray ampersands: a & b &amp; c</p>
  <img src="test.png" alt="a > b & c < d">
  <br>
  <hr>
</div>`

	cleaned := SanitizeAndWrapXHTML(raw, "advanced.html", "epub.css")

	if !strings.Contains(cleaned, "Café") {
		t.Errorf("Expected &eacute; decoded to Café, got: %s", cleaned)
	}
	if !strings.Contains(cleaned, "&amp;foobarunknown;") {
		t.Errorf("Expected unknown entity escaped, got: %s", cleaned)
	}
	if !strings.Contains(cleaned, "a &amp; b &amp; c") {
		t.Errorf("Expected stray ampersand escaped, got: %s", cleaned)
	}
	if !strings.Contains(cleaned, `alt="a > b &amp; c &lt; d"`) {
		t.Errorf("Expected < escaped in attribute, got: %s", cleaned)
	}

	var dummy struct{}
	if err := xml.Unmarshal([]byte(cleaned), &dummy); err != nil {
		t.Fatalf("Cleaned advanced doc is not valid XML: %v", err)
	}
}

func TestExtractChapterTitle(t *testing.T) {
	cases := []struct {
		html     string
		filename string
		expected string
	}{
		{`<section data-pdf-bookmark="Chapter 5. Testing"></section>`, "ch05.html", "Chapter 5. Testing"},
		{`<h1>Getting Started</h1>`, "start.html", "Getting Started"},
		{`<title>Overview Page</title>`, "ch01.html", "Overview Page"},
		{`<div>No header</div>`, "ch08.html", "Chapter 8"},
		{`<div>No header</div>`, "app02.html", "Appendix 2"},
		{`<div>No header</div>`, "titlepage01.html", "Titlepage 01"},
	}

	for _, c := range cases {
		actual := ExtractChapterTitle(c.html, c.filename)
		if actual != c.expected {
			t.Errorf("For %s (%s): expected %q, got %q", c.filename, c.html, c.expected, actual)
		}
	}
}
