package epub

import (
	_ "embed"
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DefaultEpubCSS provides the complete official O'Reilly reader stylesheet
// and typography/responsive layout rules for e-readers.
//
//go:embed default_epub.css
var DefaultEpubCSS string

// DefaultInlineCSS provides clean typography reset and layout rules injected into chapter heads.
const DefaultInlineCSS = `body {
  margin: 1em;
  line-height: 1.5;
  background-color: transparent !important;
}
#sbo-rt-content * {
  text-indent: 0pt !important;
  word-wrap: break-word !important;
  word-break: break-word !important;
}
#sbo-rt-content .bq {
  margin-right: 1em !important;
}
#sbo-rt-content table, #sbo-rt-content pre, #sbo-rt-content code {
  overflow-x: auto !important;
  white-space: pre-wrap !important;
  word-break: break-word !important;
}
img {
  max-width: 100% !important;
  height: auto !important;
}`

var (
	scriptRegex       = regexp.MustCompile(`(?i)<script\b[\s\S]*?<\/script>`)
	noscriptRegex     = regexp.MustCompile(`(?i)<noscript\b[\s\S]*?<\/noscript>`)
	botDivRegex       = regexp.MustCompile(`(?i)<div id="sec-overlay"[\s\S]*?<\/div>(\s*<\/div>)?`)
	botLinkRegex      = regexp.MustCompile(`(?i)<link[^>]+(?:TCcAz|akamai)[^>]*>`)
	dataTemplateRegex = regexp.MustCompile(`(?i)<style\b([^>]*?)data-template=["']([^"']*)["']([^>]*)>([\s\S]*?)<\/style>`)
	svgImageRegex     = regexp.MustCompile(`(?i)<svg\b[^>]*>\s*<image\b[^>]*(?:xlink:href|href)=["']([^"']+)["'][^>]*>(?:\s*<\/image>)?\s*<\/svg>`)
	imageTagRegex     = regexp.MustCompile(`(?i)<image\b[^>]*(?:xlink:href|href)=["']([^"']+)["'][^>]*>(?:\s*<\/image>)?`)
	cssContentUrlRegex = regexp.MustCompile(`(?i)content\s*:\s*url\([^)]+\)`)
	apiAssetRegex     = regexp.MustCompile(`(?i)(src|href)=["'](?:https?://learning\.oreilly\.com)?/api/v2/epubs/urn:orm:book:[^/]+/files/([^"']+)["']`)
	viewLinkRegex     = regexp.MustCompile(`(?i)href=["'](?:https?://learning\.oreilly\.com)?/library/view/[^/]+/[^/]+/([^"'#?]+)(#[^"']*)?["']`)
	namedEntityRegex       = regexp.MustCompile(`&([a-zA-Z0-9]+);`)
	validEntityPrefixRegex = regexp.MustCompile(`^&(?:amp|lt|gt|quot|apos|#\d+|#x[0-9a-fA-F]+);`)
	attrDblQuoteRegex      = regexp.MustCompile(`=\s*"([^"]*)"`)
	attrSglQuoteRegex      = regexp.MustCompile(`=\s*'([^']*)'`)
	voidTagRegex           = regexp.MustCompile(`(?i)<((?:img|br|hr|link|meta|input|source|col|area|base|embed|param|track|wbr)\b(?:[^'">]|"[^"]*"|'[^']*')*?)>`)
	bookmarkRegex     = regexp.MustCompile(`(?i)data-pdf-bookmark=["']([^"']+)["']`)
	h1Regex           = regexp.MustCompile(`(?i)<h1[^>]*>([\s\S]*?)<\/h1>`)
	titleTagRegex     = regexp.MustCompile(`(?i)<title[^>]*>([\s\S]*?)<\/title>`)
	tagStripRegex     = regexp.MustCompile(`<[^>]+>`)
	xmlDeclRegex      = regexp.MustCompile(`(?i)<\?xml\b[^>]*\?>\s*`)
	doctypeRegex      = regexp.MustCompile(`(?i)<!DOCTYPE\b[^>]*>\s*`)
	htmlTagRegex      = regexp.MustCompile(`(?i)<html\b([^>]*)>`)
	headTagRegex      = regexp.MustCompile(`(?i)<head\b([^>]*)>`)
	headEndTagRegex   = regexp.MustCompile(`(?i)</head>`)
	charsetMetaRegex  = regexp.MustCompile(`(?i)<meta\b[^>]*charset`)
)

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// ExtractChapterTitle finds human-friendly chapter titles from bookmarks, headings, or filenames.
func ExtractChapterTitle(rawHTML, filename string) string {
	if m := bookmarkRegex.FindStringSubmatch(rawHTML); len(m) > 1 && strings.TrimSpace(m[1]) != "" {
		return strings.TrimSpace(m[1])
	}
	if m := h1Regex.FindStringSubmatch(rawHTML); len(m) > 1 {
		cleaned := strings.TrimSpace(tagStripRegex.ReplaceAllString(m[1], ""))
		if cleaned != "" {
			return cleaned
		}
	}
	if m := titleTagRegex.FindStringSubmatch(rawHTML); len(m) > 1 {
		cleaned := strings.TrimSpace(tagStripRegex.ReplaceAllString(m[1], ""))
		if cleaned != "" && !strings.Contains(strings.ToLower(cleaned), "untitled") {
			return cleaned
		}
	}

	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	chRe := regexp.MustCompile(`(?i)^ch(?:apter)?_?(\d+)$`)
	if m := chRe.FindStringSubmatch(base); len(m) > 1 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return fmt.Sprintf("Chapter %d", n)
		}
	}
	appRe := regexp.MustCompile(`(?i)^app(?:endix)?_?(\d+)$`)
	if m := appRe.FindStringSubmatch(base); len(m) > 1 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return fmt.Sprintf("Appendix %d", n)
		}
	}

	digitSplitRe := regexp.MustCompile(`([a-zA-Z]+)(\d+)`)
	spacedBase := digitSplitRe.ReplaceAllString(base, "$1 $2")
	words := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(spacedBase, "-", " "), "_", " "))
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}

func escapeStrayAmpersands(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			if validEntityPrefixRegex.MatchString(s[i:]) {
				b.WriteByte('&')
			} else {
				b.WriteString("&amp;")
			}
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// ExtractContent isolates the actual book reading content:
// 1. Searches for <div id="sbo-rt-content">...</div> (used by all O'Reilly book chapters)
// 2. Falls back to inner content of <body>...</body> if present
// 3. Falls back to the raw string if already a content fragment.
func ExtractContent(raw string) string {
	sboRe := regexp.MustCompile(`(?i)<div\b[^>]*\bid=["']sbo-rt-content["'][^>]*>`)
	loc := sboRe.FindStringIndex(raw)
	if len(loc) == 2 {
		start := loc[0]
		depth := 1
		pos := loc[1]
		divTagRe := regexp.MustCompile(`(?i)</?div\b[^>]*>`)
		for {
			m := divTagRe.FindStringIndex(raw[pos:])
			if len(m) == 0 {
				break
			}
			tagStart := pos + m[0]
			tagEnd := pos + m[1]
			tag := strings.ToLower(raw[tagStart:tagEnd])
			pos = tagEnd
			if strings.HasPrefix(tag, "</div") {
				depth--
				if depth == 0 {
					extracted := raw[start:tagEnd]
					// Sanity check: ensure we didn't stop before significant body content
					bodyEndRe := regexp.MustCompile(`(?i)</body>`)
					if bEnd := bodyEndRe.FindStringIndex(raw[tagEnd:]); len(bEnd) == 2 {
						remainder := raw[tagEnd : tagEnd+bEnd[0]]
						if (strings.Contains(remainder, "</p>") || strings.Contains(remainder, "</section>")) && len(remainder) > len(extracted) {
							// Premature match due to unclosed inner tag, continue
							continue
						}
					}
					return extracted
				}
			} else if !strings.HasSuffix(tag, "/>") {
				depth++
			}
		}
		bodyEndRe := regexp.MustCompile(`(?i)</body>`)
		if bEnd := bodyEndRe.FindStringIndex(raw[start:]); len(bEnd) == 2 {
			return raw[start : start+bEnd[0]]
		}
		return raw[start:]
	}

	bodyStartRe := regexp.MustCompile(`(?i)<body\b[^>]*>`)
	bodyEndRe := regexp.MustCompile(`(?i)</body>`)
	bStart := bodyStartRe.FindStringIndex(raw)
	if len(bStart) == 2 {
		bEnd := bodyEndRe.FindStringIndex(raw[bStart[1]:])
		if len(bEnd) == 2 {
			return strings.TrimSpace(raw[bStart[1] : bStart[1]+bEnd[0]])
		}
		return strings.TrimSpace(raw[bStart[1]:])
	}

	return strings.TrimSpace(raw)
}

// SanitizeAndWrapXHTML cleans HTML fragments, extracts styles and converts SVGs,
// and wraps them in a valid EPUB 3 XHTML document.
func SanitizeAndWrapXHTML(raw string, filename string, cssHrefs ...string) string {
	// 0. Extract title from original document before stripping
	title := ExtractChapterTitle(raw, filename)
	title = escapeXML(title)

	// 1. Remove script and noscript tags
	c := scriptRegex.ReplaceAllString(raw, "")
	c = noscriptRegex.ReplaceAllString(c, "")

	// 2. Remove Akamai bot detection artifacts
	c = botDivRegex.ReplaceAllString(c, "")
	c = botLinkRegex.ReplaceAllString(c, "")

	// 3. Extract actual reading content (isolates #sbo-rt-content or <body>)
	c = ExtractContent(c)

	// If content doesn't have sbo-rt-content wrapper, add it for CSS targeting
	if !strings.Contains(c, `id="sbo-rt-content"`) && !strings.Contains(c, `id='sbo-rt-content'`) {
		c = fmt.Sprintf("<div id=\"sbo-rt-content\">\n%s\n</div>", c)
	}

	// 4. Extract <style data-template="..."> (from oreilly-ingest)
	c = dataTemplateRegex.ReplaceAllStringFunc(c, func(m string) string {
		sub := dataTemplateRegex.FindStringSubmatch(m)
		if len(sub) >= 5 {
			before := sub[1]
			template := sub[2]
			after := sub[3]
			body := strings.TrimSpace(sub[4])

			unescaped := template
			unescaped = strings.ReplaceAll(unescaped, "&quot;", "\"")
			unescaped = strings.ReplaceAll(unescaped, "&apos;", "'")
			unescaped = strings.ReplaceAll(unescaped, "&lt;", "<")
			unescaped = strings.ReplaceAll(unescaped, "&gt;", ">")
			unescaped = strings.ReplaceAll(unescaped, "&amp;", "&")

			finalContent := unescaped
			if body != "" {
				finalContent = body + "\n" + unescaped
			}
			return fmt.Sprintf("<style%s%s>%s</style>", before, after, finalContent)
		}
		return m
	})

	// 5. Convert SVG <image> wrappers to <img> tags (from oreilly-ingest)
	c = svgImageRegex.ReplaceAllString(c, `<img src="$1" alt="" />`)
	c = imageTagRegex.ReplaceAllString(c, `<img src="$1" alt="" />`)

	// 6. Clean pseudo-element content: url() in CSS
	c = cssContentUrlRegex.ReplaceAllString(c, `content: ""`)

	// 7. Rewrite O'Reilly API asset URLs and web links to local relative paths
	c = apiAssetRegex.ReplaceAllString(c, `$1="$2"`)
	c = viewLinkRegex.ReplaceAllString(c, `href="$1$2"`)

	// 8. Decode HTML named entities and ensure valid XML entities
	c = namedEntityRegex.ReplaceAllStringFunc(c, func(match string) string {
		name := match[1 : len(match)-1]
		switch name {
		case "amp", "lt", "gt", "quot", "apos":
			return match
		case "nbsp":
			return "&#160;"
		case "ensp":
			return "&#8194;"
		case "emsp":
			return "&#8195;"
		case "thinsp":
			return "&#8201;"
		case "zwnj":
			return "&#8204;"
		case "zwj":
			return "&#8205;"
		}

		unescaped := html.UnescapeString(match)
		if unescaped != match {
			if unescaped == "<" {
				return "&lt;"
			}
			if unescaped == ">" {
				return "&gt;"
			}
			if unescaped == "&" {
				return "&amp;"
			}
			return unescaped
		}
		return "&amp;" + name + ";"
	})

	// Escape any remaining stray ampersands
	c = escapeStrayAmpersands(c)

	// Escape literal < inside attribute values
	c = attrDblQuoteRegex.ReplaceAllStringFunc(c, func(m string) string {
		val := m[strings.IndexByte(m, '"')+1 : len(m)-1]
		return fmt.Sprintf(`="%s"`, strings.ReplaceAll(val, "<", "&lt;"))
	})
	c = attrSglQuoteRegex.ReplaceAllStringFunc(c, func(m string) string {
		val := m[strings.IndexByte(m, '\'')+1 : len(m)-1]
		return fmt.Sprintf(`='%s'`, strings.ReplaceAll(val, "<", "&lt;"))
	})

	// 9. Self-close void HTML tags
	c = voidTagRegex.ReplaceAllStringFunc(c, func(m string) string {
		inner := m[1 : len(m)-1]
		trimmed := strings.TrimRight(inner, " \t\r\n/")
		return "<" + trimmed + " />"
	})

	var cssLinksBuilder strings.Builder
	for _, href := range cssHrefs {
		if href != "" {
			cssLinksBuilder.WriteString(fmt.Sprintf("  <link rel=\"stylesheet\" type=\"text/css\" href=\"%s\" />\n", escapeXML(href)))
		}
	}
	baseStyleTag := fmt.Sprintf("  <style type=\"text/css\">\n%s\n  </style>\n", DefaultInlineCSS)

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xmlns:m="http://www.w3.org/1998/Math/MathML" xmlns:svg="http://www.w3.org/2000/svg" xml:lang="en" lang="en">
<head>
  <meta charset="utf-8" />
  <title>%s</title>
%s%s</head>
<body>
%s
</body>
</html>`, title, cssLinksBuilder.String(), baseStyleTag, strings.TrimSpace(c))
}
