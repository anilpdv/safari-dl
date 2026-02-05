package downloader

import (
	"encoding/json"
	"strings"
)

type FlexibleAuthors []string

func (fa *FlexibleAuthors) UnmarshalJSON(data []byte) error {
	// Try []string
	var strSlice []string
	if err := json.Unmarshal(data, &strSlice); err == nil {
		*fa = strSlice
		return nil
	}

	// Try []struct{ Name string }
	var objSlice []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &objSlice); err == nil {
		var names []string
		for _, o := range objSlice {
			if strings.TrimSpace(o.Name) != "" {
				names = append(names, strings.TrimSpace(o.Name))
			}
		}
		*fa = names
		return nil
	}

	// Try single string
	var singleStr string
	if err := json.Unmarshal(data, &singleStr); err == nil {
		if strings.TrimSpace(singleStr) != "" {
			*fa = []string{strings.TrimSpace(singleStr)}
		}
		return nil
	}

	*fa = nil
	return nil
}

type Manifest struct {
	Title     string          `json:"title"`
	Authors   FlexibleAuthors `json:"authors"`
	ISBN      string          `json:"isbn"`
	Files     string          `json:"files"`
	TotalSize int64           `json:"total_size"`
}

type FileInfo struct {
	URL      string `json:"url"`
	FullPath string `json:"full_path"`
}

type FilesPageResponse struct {
	Count    int        `json:"count"`
	Next     *string    `json:"next"`
	Previous *string    `json:"previous"`
	Results  []FileInfo `json:"results"`
}

type TOCItem struct {
	ReferenceID string    `json:"reference_id"`
	Href        string    `json:"href"`
	Title       string    `json:"title"`
	Fragment    string    `json:"fragment"`
	Children    []TOCItem `json:"children"`
}

type DownloadOptions struct {
	BookID       string
	Token        string
	CookieHeader string
	OutputDir    string
	Concurrency  int
	EmitJSON     bool
}

type ProgressEvent struct {
	Type                string  `json:"type"` // "progress", "completed", "error"
	Percent             float64 `json:"percent"`
	CurrentChapterIndex int     `json:"currentChapterIndex"`
	TotalChapters       int     `json:"totalChapters"`
	CurrentChapterTitle string  `json:"currentChapterTitle"`
	SpeedBytes          float64 `json:"speedBytes"`
	SpeedFormatted      string  `json:"speedFormatted"`
	ETA                 string  `json:"eta"`
	DownloadedBytes     int64   `json:"downloadedBytes"`
	TotalBytes          int64   `json:"totalBytes"`
	OutputPath          string  `json:"outputPath,omitempty"`
	Error               string  `json:"error,omitempty"`
}
