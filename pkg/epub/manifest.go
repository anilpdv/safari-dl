package epub

import (
	"fmt"
	"html"
	"path/filepath"
	"strings"
	"time"
)

type ManifestItem struct {
	ID         string
	Href       string
	MediaType  string
	Properties string
}

func GetMediaType(relPath string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(relPath), "."))
	switch ext {
	case "html", "xhtml":
		return "application/xhtml+xml"
	case "css":
		return "text/css"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "svg":
		return "image/svg+xml"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "ncx":
		return "application/x-dtbncx+xml"
	case "otf":
		return "font/otf"
	case "ttf":
		return "font/ttf"
	case "woff":
		return "font/woff"
	case "woff2":
		return "font/woff2"
	case "js":
		return "application/javascript"
	default:
		return "application/octet-stream"
	}
}

func GenerateContainerXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`
}

func GenerateContentOPF(bookID, title string, authors []string, items []ManifestItem, spineIDs []string, hasNCX bool, coverItemID string) string {
	var manifestBuilder strings.Builder
	for _, it := range items {
		propAttr := ""
		if it.Properties != "" {
			propAttr = fmt.Sprintf(` properties="%s"`, it.Properties)
		}
		manifestBuilder.WriteString(fmt.Sprintf(`    <item id="%s" href="%s" media-type="%s"%s/>`+"\n", it.ID, it.Href, it.MediaType, propAttr))
	}

	var spineBuilder strings.Builder
	for _, id := range spineIDs {
		spineBuilder.WriteString(fmt.Sprintf(`    <itemref idref="%s"/>`+"\n", id))
	}

	tocAttr := ""
	if hasNCX {
		tocAttr = ` toc="ncx"`
	}

	coverMeta := ""
	if coverItemID != "" {
		coverMeta = fmt.Sprintf(`    <meta name="cover" content="%s"/>`+"\n", coverItemID)
	}

	var authorBuilder strings.Builder
	for _, a := range authors {
		authorBuilder.WriteString(fmt.Sprintf(`    <dc:creator>%s</dc:creator>`+"\n", html.EscapeString(a)))
	}

	modTime := time.Now().UTC().Format(time.RFC3339)

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" unique-identifier="pub-id" version="3.0" prefix="rendition: http://www.idpf.org/vocab/rendition/#">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="pub-id">urn:isbn:%s</dc:identifier>
    <dc:title>%s</dc:title>
%s    <dc:language>en</dc:language>
    <meta property="dcterms:modified">%s</meta>
%s  </metadata>
  <manifest>
%s  </manifest>
  <spine%s>
%s  </spine>
</package>`, bookID, html.EscapeString(title), authorBuilder.String(), modTime, coverMeta, manifestBuilder.String(), tocAttr, spineBuilder.String())
}

type SpineEntry struct {
	Href  string
	Title string
}

func GenerateNavXHTML(title string, spineEntries []SpineEntry) string {
	var itemsBuilder strings.Builder
	for _, entry := range spineEntries {
		itemsBuilder.WriteString(fmt.Sprintf(`      <li><a href="%s">%s</a></li>`+"\n", entry.Href, html.EscapeString(entry.Title)))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="en" lang="en">
<head>
  <meta charset="utf-8" />
  <title>%s - Table of Contents</title>
</head>
<body>
  <nav epub:type="toc" id="toc">
    <h1>Table of Contents</h1>
    <ol>
%s    </ol>
  </nav>
</body>
</html>`, html.EscapeString(title), itemsBuilder.String())
}

func GenerateNCX(bookID, title string, spineEntries []SpineEntry) string {
	var pointsBuilder strings.Builder
	for i, entry := range spineEntries {
		pointsBuilder.WriteString(fmt.Sprintf(`    <navPoint id="navpoint-%d" playOrder="%d">
      <navLabel><text>%s</text></navLabel>
      <content src="%s"/>
    </navPoint>`+"\n", i+1, i+1, html.EscapeString(entry.Title), entry.Href))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <head>
    <meta name="dtb:uid" content="urn:isbn:%s"/>
    <meta name="dtb:depth" content="1"/>
    <meta name="dtb:totalPageCount" content="0"/>
    <meta name="dtb:maxPageNumber" content="0"/>
  </head>
  <docTitle><text>%s</text></docTitle>
  <navMap>
%s  </navMap>
</ncx>`, bookID, html.EscapeString(title), pointsBuilder.String())
}
