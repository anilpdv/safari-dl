package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"safari-dl/pkg/downloader"
)

type AppSettings struct {
	DownloadDir string `json:"downloadDir"`
	Cookies     string `json:"cookies"`
	Concurrency int    `json:"concurrency"`
}

func loadSettingsFromConfig() (*AppSettings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	settingsPath := filepath.Join(home, ".config", "course-book-downloader", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil, err
	}
	var s AppSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func parseCookies(raw string) (string, string) {
	lines := strings.Split(raw, "\n")
	cookieMap := make(map[string]string)
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.Split(trimmed, "\t")
		if len(parts) >= 7 {
			name := strings.TrimSpace(parts[5])
			val := strings.TrimSpace(parts[6])
			cookieMap[name] = val
		}
	}
	var cookiePairs []string
	for k, v := range cookieMap {
		cookiePairs = append(cookiePairs, fmt.Sprintf("%s=%s", k, v))
	}
	token := cookieMap["orm-jwt"]
	return token, strings.Join(cookiePairs, "; ")
}

func extractBookID(input string) string {
	input = strings.Trim(strings.TrimSpace(input), "/")
	re := regexp.MustCompile(`(?i)(?:book:|urn:orm:book:|/view/[^/]+/|/course/[^/]+/|/videos/[^/]+/|/library/view/[^/]+/|/covers?/|/)(\d{10,13}[A-Z]*|\d{9}[0-9X])`)
	if m := re.FindStringSubmatch(input); len(m) > 1 {
		return m[1]
	}
	return input
}

func main() {
	bookFlag := flag.String("book", "", "O'Reilly Book ID, ISBN, or web URL")
	cookieFileFlag := flag.String("cookies", "", "Path to Netscape cookies.txt file")
	cookieHeaderFlag := flag.String("cookie-header", "", "Raw Cookie header string (key1=val1; key2=val2)")
	tokenFlag := flag.String("token", "", "O'Reilly orm-jwt Bearer token")
	outDirFlag := flag.String("out", "", "Output directory for downloaded EPUB")
	concurrencyFlag := flag.Int("concurrency", 16, "Number of concurrent download workers")
	jsonFlag := flag.Bool("json", false, "Emit machine-readable NDJSON progress events")
	flag.Parse()

	if *bookFlag == "" {
		if flag.NArg() > 0 {
			*bookFlag = flag.Arg(0)
		} else {
			fmt.Fprintln(os.Stderr, "Usage: safari-dl -book <id_or_url> [-out <dir>] [-concurrency <n>] [-json]")
			os.Exit(1)
		}
	}

	bookID := extractBookID(*bookFlag)
	if bookID == "" {
		fmt.Fprintln(os.Stderr, "Error: Invalid book identifier")
		os.Exit(1)
	}

	token := *tokenFlag
	cookieHeader := ""

	if *cookieHeaderFlag != "" {
		cookieHeader = *cookieHeaderFlag
	}

	// If cookie file given, read it
	if *cookieFileFlag != "" {
		data, err := os.ReadFile(*cookieFileFlag)
		if err == nil {
			t, c := parseCookies(string(data))
			if token == "" {
				token = t
			}
			if cookieHeader == "" {
				cookieHeader = c
			}
		}
	}

	// Extract token from cookie header if token missing
	if token == "" && cookieHeader != "" {
		for _, pair := range strings.Split(cookieHeader, ";") {
			parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(parts) == 2 && parts[0] == "orm-jwt" {
				token = parts[1]
				break
			}
		}
	}

	// Auto-detect from settings if credentials missing
	if token == "" || cookieHeader == "" {
		settings, err := loadSettingsFromConfig()
		if err == nil && settings != nil {
			t, c := parseCookies(settings.Cookies)
			if token == "" {
				token = t
			}
			if cookieHeader == "" {
				cookieHeader = c
			}
			if *outDirFlag == "" && settings.DownloadDir != "" {
				*outDirFlag = settings.DownloadDir
			}
		}
	}

	outDir := *outDirFlag
	if outDir == "" || strings.Contains(outDir, "Libation") {
		home, _ := os.UserHomeDir()
		defaultDir := filepath.Join(home, "Downloads", "OReilly", "Books")
		_ = os.MkdirAll(defaultDir, 0755)
		outDir = defaultDir
	}

	opts := downloader.DownloadOptions{
		BookID:       bookID,
		Token:        token,
		CookieHeader: cookieHeader,
		OutputDir:    outDir,
		Concurrency:  *concurrencyFlag,
		EmitJSON:     *jsonFlag,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client := downloader.NewClient(token, cookieHeader)

	progressCallback := func(evt downloader.ProgressEvent) {
		if *jsonFlag {
			data, _ := json.Marshal(evt)
			fmt.Println(string(data))
		} else {
			if evt.Type == "completed" {
				fmt.Printf("\n[100%%] Successfully packaged EPUB: %s\n", evt.OutputPath)
			} else if evt.Type == "error" {
				fmt.Fprintf(os.Stderr, "\n[ERROR] %s\n", evt.Error)
			} else {
				fmt.Printf("\r[%5.1f%%] %d/%d (%s) - %s ETA: %s",
					evt.Percent,
					evt.CurrentChapterIndex,
					evt.TotalChapters,
					evt.SpeedFormatted,
					evt.CurrentChapterTitle,
					evt.ETA,
				)
			}
		}
	}

	outPath, err := downloader.DownloadBook(ctx, client, opts, progressCallback)
	if err != nil {
		if *jsonFlag {
			errEvt := downloader.ProgressEvent{
				Type:  "error",
				Error: err.Error(),
			}
			data, _ := json.Marshal(errEvt)
			fmt.Println(string(data))
		} else {
			fmt.Fprintf(os.Stderr, "\nDownload failed: %v\n", err)
		}
		os.Exit(1)
	}

	_ = outPath
}
