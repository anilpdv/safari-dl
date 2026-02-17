package downloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"safari-dl/pkg/epub"
)

type SpeedTracker struct {
	mu      sync.Mutex
	history []transferEntry
}

type transferEntry struct {
	timestamp time.Time
	bytes     int64
}

func (st *SpeedTracker) Record(bytes int64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	now := time.Now()
	st.history = append(st.history, transferEntry{timestamp: now, bytes: bytes})
	cutoff := now.Add(-1500 * time.Millisecond)
	idx := 0
	for idx < len(st.history) && st.history[idx].timestamp.Before(cutoff) {
		idx++
	}
	st.history = st.history[idx:]
}

func (st *SpeedTracker) CurrentSpeed() float64 {
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.history) < 2 {
		return 0
	}
	var total int64
	for _, e := range st.history {
		total += e.bytes
	}
	duration := st.history[len(st.history)-1].timestamp.Sub(st.history[0].timestamp).Seconds()
	if duration <= 0 {
		return 0
	}
	return float64(total) / duration
}

func FormatSpeed(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return "0 KiB/s"
	}
	if bytesPerSec >= 1024*1024 {
		return fmt.Sprintf("%.2f MiB/s", bytesPerSec/(1024*1024))
	}
	return fmt.Sprintf("%.1f KiB/s", bytesPerSec/1024)
}

func FormatETA(seconds float64) string {
	if seconds <= 0 || seconds > 86400 {
		return "--:--"
	}
	m := int(seconds) / 60
	s := int(seconds) % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

func SanitizeFilename(name string) string {
	re := regexp.MustCompile(`[/\\?%*:|"<>]+`)
	clean := re.ReplaceAllString(name, "-")
	return strings.TrimSpace(clean)
}

var frontMatterKeywords = []string{"cover", "halftitle", "titlepage", "title-page", "contents", "toc"}
var backMatterKeywords = []string{"index", "colophon", "afterword", "about-the-author", "author"}

var teaserRegex = regexp.MustCompile(`(?i)(?:\.\.\.|…)\s*</(?:p|div|section)>`)

func isPreviewTeaser(content, filename string) bool {
	lower := strings.ToLower(filename)
	if strings.Contains(lower, "cover") || strings.Contains(lower, "title") || strings.Contains(lower, "copyright") || strings.Contains(lower, "toc") || strings.Contains(lower, "colophon") {
		return false
	}
	if strings.Contains(content, `class="preview-edition"`) || strings.Contains(content, `id="preview-edition"`) {
		return true
	}
	if len(content) < 5000 && teaserRegex.MatchString(content) {
		return true
	}
	return false
}


// FlattenTOCFilenames extracts chapter filenames from the TOC tree.
func FlattenTOCFilenames(items []TOCItem) []string {
	var result []string
	seen := make(map[string]bool)

	var walk func(nodes []TOCItem)
	walk = func(nodes []TOCItem) {
		for _, n := range nodes {
			ref := n.ReferenceID
			if ref == "" {
				ref = n.Href
			}
			if idx := strings.Index(ref, "-/"); idx != -1 {
				ref = ref[idx+2:]
			}
			if idx := strings.IndexAny(ref, "?#"); idx != -1 {
				ref = ref[:idx]
			}
			ref = strings.TrimPrefix(filepath.ToSlash(ref), "/")
			if ref != "" && !seen[ref] {
				result = append(result, ref)
				seen[ref] = true
			}
			if len(n.Children) > 0 {
				walk(n.Children)
			}
		}
	}

	walk(items)
	return result
}

// ReorderByTOC sorts filenames: Front matter -> Canonical TOC order -> Remaining -> Back matter.
func ReorderByTOC(allFilenames, tocFilenames []string) []string {
	tocSet := make(map[string]bool)
	for _, fn := range tocFilenames {
		tocSet[fn] = true
	}

	remainingSet := make(map[string]bool)
	for _, fn := range allFilenames {
		remainingSet[fn] = true
	}

	var front, middle, back []string

	for _, fn := range allFilenames {
		if !tocSet[fn] {
			lower := strings.ToLower(fn)
			isFront := false
			for _, k := range frontMatterKeywords {
				if strings.Contains(lower, k) {
					isFront = true
					break
				}
			}
			if isFront {
				front = append(front, fn)
				delete(remainingSet, fn)
				continue
			}
			isBack := false
			for _, k := range backMatterKeywords {
				if strings.Contains(lower, k) {
					isBack = true
					break
				}
			}
			if isBack {
				back = append(back, fn)
				delete(remainingSet, fn)
			}
		}
	}

	for _, fn := range tocFilenames {
		if remainingSet[fn] {
			middle = append(middle, fn)
			delete(remainingSet, fn)
		}
	}

	for _, fn := range allFilenames {
		if remainingSet[fn] {
			middle = append(middle, fn)
			delete(remainingSet, fn)
		}
	}

	res := append(front, middle...)
	return append(res, back...)
}

// DownloadBook executes the full concurrent download and EPUB packaging pipeline.
func DownloadBook(ctx context.Context, client *Client, opts DownloadOptions, onProgress func(ProgressEvent)) (string, error) {
	cleanID := strings.TrimSuffix(opts.BookID, "VE")

	onProgress(ProgressEvent{
		Type:                "progress",
		Percent:             1,
		CurrentChapterTitle: "Fetching book manifest...",
	})

	manifest, err := client.FetchManifest(ctx, cleanID)
	if err != nil {
		return "", fmt.Errorf("failed fetching manifest: %w", err)
	}

	// Fetch TOC for canonical chapter reading sequence
	tocItems, _ := client.FetchTOC(ctx, cleanID)

	filesURL := manifest.Files
	if filesURL == "" {
		filesURL = fmt.Sprintf("https://learning.oreilly.com/api/v2/epubs/urn:orm:book:%s/files/", cleanID)
	}

	onProgress(ProgressEvent{
		Type:                "progress",
		Percent:             3,
		CurrentChapterTitle: "Fetching all book files list...",
	})

	allFiles, err := client.FetchAllFiles(ctx, filesURL)
	if err != nil {
		return "", fmt.Errorf("failed fetching files list: %w", err)
	}
	if len(allFiles) == 0 {
		return "", fmt.Errorf("no files found in book")
	}

	totalFiles := len(allFiles)
	totalBytes := manifest.TotalSize
	if totalBytes <= 0 {
		totalBytes = int64(totalFiles * 65 * 1024)
	}

	// Setup temporary build directory
	buildDir, err := os.MkdirTemp("", fmt.Sprintf("epub_build_%s_*", cleanID))
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(buildDir)

	metaInfDir := filepath.Join(buildDir, "META-INF")
	oebpsDir := filepath.Join(buildDir, "OEBPS")
	stylesDir := filepath.Join(oebpsDir, "Styles")
	if err := os.MkdirAll(metaInfDir, 0755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(oebpsDir, 0755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(stylesDir, 0755); err != nil {
		return "", err
	}

	// Write standard mimetype and container.xml
	if err := os.WriteFile(filepath.Join(buildDir, "mimetype"), []byte("application/epub+zip"), 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(metaInfDir, "container.xml"), []byte(epub.GenerateContainerXML()), 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(stylesDir, "default.css"), []byte(epub.DefaultEpubCSS), 0644); err != nil {
		return "", err
	}

	// Concurrency worker setup
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 16
	}
	if concurrency > totalFiles {
		concurrency = totalFiles
	}

	// Collect all CSS files present in the book manifest
	var allCSSFiles []string
	for _, f := range allFiles {
		p := f.FullPath
		if p == "" {
			parts := strings.Split(f.URL, "/files/")
			if len(parts) > 1 {
				p = parts[1]
			}
		}
		if idx := strings.IndexAny(p, "?#"); idx != -1 {
			p = p[:idx]
		}
		p = strings.TrimPrefix(filepath.ToSlash(p), "/")
		if strings.HasSuffix(strings.ToLower(p), ".css") {
			allCSSFiles = append(allCSSFiles, p)
		}
	}

	var (
		downloadedBytes int64
		completedFiles  int64
		speedTracker    SpeedTracker
	)

	type taskItem struct {
		index    int
		fileInfo FileInfo
	}

	taskChan := make(chan taskItem, totalFiles)
	for i, f := range allFiles {
		taskChan <- taskItem{index: i, fileInfo: f}
	}
	close(taskChan)

	var wg sync.WaitGroup
	var workerErr error
	var errMu sync.Mutex
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				select {
				case <-workerCtx.Done():
					return
				default:
				}

				f := task.fileInfo
				relPath := f.FullPath
				if relPath == "" {
					parts := strings.Split(f.URL, "/files/")
					if len(parts) > 1 {
						relPath = parts[1]
					} else {
						relPath = fmt.Sprintf("chapter_%d.html", task.index)
					}
				}
				// Strip query string and fragment
				if idx := strings.IndexAny(relPath, "?#"); idx != -1 {
					relPath = relPath[:idx]
				}
				relPath = strings.TrimPrefix(filepath.ToSlash(relPath), "/")

				// Skip upstream opf/mimetype
				if relPath == "content.opf" || relPath == "mimetype" {
					atomic.AddInt64(&completedFiles, 1)
					continue
				}

				data, err := client.DownloadBlob(workerCtx, f.URL, 3)
				if err != nil {
					if strings.Contains(err.Error(), "authentication") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
						errMu.Lock()
						if workerErr == nil {
							workerErr = err
						}
						errMu.Unlock()
						cancelWorkers()
						return
					}
					atomic.AddInt64(&completedFiles, 1)
					continue
				}

				destFile := filepath.Join(oebpsDir, relPath)
				_ = os.MkdirAll(filepath.Dir(destFile), 0755)

				isHTML := strings.HasSuffix(relPath, ".html") || strings.HasSuffix(relPath, ".xhtml")
				if isHTML {
					fromDir := filepath.Dir(relPath)
					relDefault, err := filepath.Rel(fromDir, "Styles/default.css")
					var cssHrefs []string
					if err == nil {
						cssHrefs = append(cssHrefs, filepath.ToSlash(relDefault))
					} else {
						cssHrefs = append(cssHrefs, "Styles/default.css")
					}
					for _, cssF := range allCSSFiles {
						relCSS, err := filepath.Rel(fromDir, cssF)
						if err == nil {
							cssHrefs = append(cssHrefs, filepath.ToSlash(relCSS))
						} else {
							cssHrefs = append(cssHrefs, cssF)
						}
					}
					cleanContent := epub.SanitizeAndWrapXHTML(string(data), filepath.Base(relPath), cssHrefs...)

					// Reject preview teasers if session token expired or title restricted
					if isPreviewTeaser(cleanContent, relPath) {
						errMu.Lock()
						if workerErr == nil {
							workerErr = fmt.Errorf("O'Reilly served a preview teaser for %s instead of full chapter text (session token expired or title unauthorized). Please log in again via Settings", relPath)
						}
						errMu.Unlock()
						cancelWorkers()
						return
					}

					_ = os.WriteFile(destFile, []byte(cleanContent), 0644)
				} else {
					_ = os.WriteFile(destFile, data, 0644)
				}

				nBytes := int64(len(data))
				atomic.AddInt64(&downloadedBytes, nBytes)
				speedTracker.Record(nBytes)
				cFiles := atomic.AddInt64(&completedFiles, 1)

				curSpeed := speedTracker.CurrentSpeed()
				percent := float64(cFiles) / float64(totalFiles) * 95.0
				remFiles := int64(totalFiles) - cFiles
				avgPerFile := float64(atomic.LoadInt64(&downloadedBytes)) / float64(cFiles)
				etaSec := 0.0
				if curSpeed > 0 {
					etaSec = (float64(remFiles) * avgPerFile) / curSpeed
				}

				onProgress(ProgressEvent{
					Type:                "progress",
					Percent:             percent,
					CurrentChapterIndex: int(cFiles),
					TotalChapters:       totalFiles,
					CurrentChapterTitle: relPath,
					SpeedBytes:          curSpeed,
					SpeedFormatted:      FormatSpeed(curSpeed),
					ETA:                 FormatETA(etaSec),
					DownloadedBytes:     atomic.LoadInt64(&downloadedBytes),
					TotalBytes:          totalBytes,
				})
			}
		}()
	}

	wg.Wait()
	if workerErr != nil {
		return "", workerErr
	}

	onProgress(ProgressEvent{
		Type:                "progress",
		Percent:             96,
		CurrentChapterTitle: "Building EPUB navigation & metadata...",
	})

	// Collect all files in OEBPS for manifest
	var (
		manifestItems []epub.ManifestItem
		spineEntries  []epub.SpineEntry
		spineIDs      []string
		hasNCX        bool
		hasNav        bool
		coverItemID   string
	)

	itemMap := make(map[string]string) // relSlash -> itemID
	tocNavRegex := regexp.MustCompile(`(?i)<nav\b[^>]*epub:type=["']toc["']`)

	_ = filepath.Walk(oebpsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(oebpsDir, path)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == "content.opf" || relSlash == "mimetype" {
			return nil
		}

		mediaType := epub.GetMediaType(relSlash)
		itemID := fmt.Sprintf("item_%d", len(manifestItems)+1)
		props := ""

		if relSlash == "toc.ncx" {
			hasNCX = true
			itemID = "ncx"
		} else if strings.Contains(relSlash, "cover") && strings.HasPrefix(mediaType, "image/") {
			props = "cover-image"
			coverItemID = "cover-image"
			itemID = "cover-image"
		}

		isHTML := mediaType == "application/xhtml+xml"
		if isHTML {
			content, _ := os.ReadFile(path)
			if tocNavRegex.Match(content) {
				props = "nav"
				hasNav = true
			}
		}

		itemMap[relSlash] = itemID
		manifestItems = append(manifestItems, epub.ManifestItem{
			ID:         itemID,
			Href:       relSlash,
			MediaType:  mediaType,
			Properties: props,
		})
		return nil
	})

	// Collect all HTML chapter relative paths
	var allHTMLPaths []string
	for _, f := range allFiles {
		p := f.FullPath
		if p == "" {
			parts := strings.Split(f.URL, "/files/")
			if len(parts) > 1 {
				p = parts[1]
			}
		}
		if idx := strings.IndexAny(p, "?#"); idx != -1 {
			p = p[:idx]
		}
		relSlash := strings.TrimPrefix(filepath.ToSlash(p), "/")
		if epub.GetMediaType(relSlash) == "application/xhtml+xml" {
			allHTMLPaths = append(allHTMLPaths, relSlash)
		}
	}
	for itemHref := range itemMap {
		if epub.GetMediaType(itemHref) == "application/xhtml+xml" && itemHref != "nav.xhtml" {
			found := false
			for _, h := range allHTMLPaths {
				if h == itemHref {
					found = true
					break
				}
			}
			if !found {
				allHTMLPaths = append(allHTMLPaths, itemHref)
			}
		}
	}

	// Canonical TOC chapter ordering
	tocFilenames := FlattenTOCFilenames(tocItems)
	orderedChapters := ReorderByTOC(allHTMLPaths, tocFilenames)

	seenSpine := make(map[string]bool)
	for _, relSlash := range orderedChapters {
		if seenSpine[relSlash] {
			continue
		}
		if itemID, exists := itemMap[relSlash]; exists {
			content, err := os.ReadFile(filepath.Join(oebpsDir, relSlash))
			if err == nil {
				title := epub.ExtractChapterTitle(string(content), filepath.Base(relSlash))
				spineEntries = append(spineEntries, epub.SpineEntry{
					Href:  relSlash,
					Title: title,
				})
				spineIDs = append(spineIDs, itemID)
				seenSpine[relSlash] = true
			}
		}
	}

	// Append any other HTML files found in OEBPS that were not yet in spine
	for _, item := range manifestItems {
		if item.MediaType == "application/xhtml+xml" && !seenSpine[item.Href] && item.Href != "nav.xhtml" {
			content, _ := os.ReadFile(filepath.Join(oebpsDir, item.Href))
			title := epub.ExtractChapterTitle(string(content), filepath.Base(item.Href))
			spineEntries = append(spineEntries, epub.SpineEntry{
				Href:  item.Href,
				Title: title,
			})
			spineIDs = append(spineIDs, item.ID)
			seenSpine[item.Href] = true
		}
	}

	// If nav.xhtml is missing, generate it
	if !hasNav {
		navHTML := epub.GenerateNavXHTML(manifest.Title, spineEntries)
		_ = os.WriteFile(filepath.Join(oebpsDir, "nav.xhtml"), []byte(navHTML), 0644)
		manifestItems = append(manifestItems, epub.ManifestItem{
			ID:         "nav",
			Href:       "nav.xhtml",
			MediaType:  "application/xhtml+xml",
			Properties: "nav",
		})
	}

	// If toc.ncx is missing, generate it
	if !hasNCX {
		ncxXML := epub.GenerateNCX(cleanID, manifest.Title, spineEntries)
		_ = os.WriteFile(filepath.Join(oebpsDir, "toc.ncx"), []byte(ncxXML), 0644)
		manifestItems = append(manifestItems, epub.ManifestItem{
			ID:        "ncx",
			Href:      "toc.ncx",
			MediaType: "application/x-dtbncx+xml",
		})
		hasNCX = true
	}

	// Write content.opf
	authors := manifest.Authors
	if len(authors) == 0 {
		authors = []string{"O'Reilly Author"}
	}
	opfContent := epub.GenerateContentOPF(cleanID, manifest.Title, authors, manifestItems, spineIDs, hasNCX, coverItemID)
	if err := os.WriteFile(filepath.Join(oebpsDir, "content.opf"), []byte(opfContent), 0644); err != nil {
		return "", err
	}

	// Package EPUB
	onProgress(ProgressEvent{
		Type:                "progress",
		Percent:             98,
		CurrentChapterTitle: "Packaging compliant EPUB document...",
	})

	safeName := SanitizeFilename(manifest.Title)
	if safeName == "" {
		safeName = cleanID
	}
	outPath := filepath.Join(opts.OutputDir, safeName+".epub")

	if err := epub.PackageEPUB(buildDir, outPath); err != nil {
		return "", fmt.Errorf("failed to package EPUB: %w", err)
	}

	// 7. Optimize layout and formatting with Calibre if available
	if epub.FindCalibreBinary() != "" {
		onProgress(ProgressEvent{
			Type:                "progress",
			Percent:             99,
			CurrentChapterTitle: "Optimizing layout & formatting with Calibre...",
		})
		epub.OptimizeEPUBWithCalibre(outPath)
	}

	onProgress(ProgressEvent{
		Type:                "completed",
		Percent:             100,
		CurrentChapterIndex: totalFiles,
		TotalChapters:       totalFiles,
		CurrentChapterTitle: "Ready to open in Apple Books",
		SpeedBytes:          0,
		SpeedFormatted:      "Completed",
		ETA:                 "00:00",
		DownloadedBytes:     atomic.LoadInt64(&downloadedBytes),
		TotalBytes:          atomic.LoadInt64(&downloadedBytes),
		OutputPath:          outPath,
	})

	return outPath, nil
}
