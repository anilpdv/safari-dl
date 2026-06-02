package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestAndPackager(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "epub_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	metaInf := filepath.Join(tempDir, "META-INF")
	oebps := filepath.Join(tempDir, "OEBPS")
	_ = os.MkdirAll(metaInf, 0755)
	_ = os.MkdirAll(oebps, 0755)

	// mimetype and container
	_ = os.WriteFile(filepath.Join(tempDir, "mimetype"), []byte("application/epub+zip"), 0644)
	_ = os.WriteFile(filepath.Join(metaInf, "container.xml"), []byte(GenerateContainerXML()), 0644)

	// ch1
	ch1 := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>Ch 1</title></head>
<body><section epub:type="chapter"><h1>Chapter 1</h1></section></body></html>`
	_ = os.WriteFile(filepath.Join(oebps, "ch01.xhtml"), []byte(ch1), 0644)

	// nav
	nav := GenerateNavXHTML("Test Book", []SpineEntry{{Href: "ch01.xhtml", Title: "Chapter 1"}})
	_ = os.WriteFile(filepath.Join(oebps, "nav.xhtml"), []byte(nav), 0644)

	// ncx
	ncx := GenerateNCX("12345", "Test Book", []SpineEntry{{Href: "ch01.xhtml", Title: "Chapter 1"}})
	_ = os.WriteFile(filepath.Join(oebps, "toc.ncx"), []byte(ncx), 0644)

	// opf
	items := []ManifestItem{
		{ID: "ch01", Href: "ch01.xhtml", MediaType: "application/xhtml+xml"},
		{ID: "nav", Href: "nav.xhtml", MediaType: "application/xhtml+xml", Properties: "nav"},
		{ID: "ncx", Href: "toc.ncx", MediaType: "application/x-dtbncx+xml"},
	}
	opf := GenerateContentOPF("12345", "Test Book", []string{"Author"}, items, []string{"ch01"}, true, "")
	_ = os.WriteFile(filepath.Join(oebps, "content.opf"), []byte(opf), 0644)

	// Verify all generated XML is well-formed
	var dummy struct{}
	if err := xml.Unmarshal([]byte(nav), &dummy); err != nil {
		t.Fatalf("Nav is invalid XML: %v", err)
	}
	if err := xml.Unmarshal([]byte(ncx), &dummy); err != nil {
		t.Fatalf("NCX is invalid XML: %v", err)
	}
	if err := xml.Unmarshal([]byte(opf), &dummy); err != nil {
		t.Fatalf("OPF is invalid XML: %v", err)
	}

	// Package EPUB
	outEpub := filepath.Join(tempDir, "test.epub")
	if err := PackageEPUB(tempDir, outEpub); err != nil {
		t.Fatalf("PackageEPUB failed: %v", err)
	}

	// Inspect ZIP
	zr, err := zip.OpenReader(outEpub)
	if err != nil {
		t.Fatalf("Failed reading generated EPUB zip: %v", err)
	}
	defer zr.Close()

	if len(zr.File) == 0 {
		t.Fatalf("EPUB zip is empty")
	}

	// mimetype must be first and uncompressed (Store)
	first := zr.File[0]
	if first.Name != "mimetype" {
		t.Errorf("First entry is %s, expected mimetype", first.Name)
	}
	if first.Method != zip.Store {
		t.Errorf("mimetype is compressed (method %d), expected Store (%d)", first.Method, zip.Store)
	}

	rc, _ := first.Open()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(rc)
	rc.Close()
	if strings.TrimSpace(buf.String()) != "application/epub+zip" {
		t.Errorf("Unexpected mimetype content: %q", buf.String())
	}
}
