package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	httpClient   *http.Client
	token        string
	cookieHeader string
}

func NewClient(token, cookieHeader string) *Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 30,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
	}

	return &Client{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second,
		},
		token:        token,
		cookieHeader: cookieHeader,
	}
}

func (c *Client) newRequest(ctx context.Context, method, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://learning.oreilly.com/home/")
	if c.cookieHeader != "" {
		req.Header.Set("Cookie", c.cookieHeader)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *Client) FetchManifest(ctx context.Context, bookID string) (*Manifest, error) {
	cleanID := strings.TrimSuffix(bookID, "VE")
	manifestURL := fmt.Sprintf("https://learning.oreilly.com/api/v2/epubs/urn:orm:book:%s/", cleanID)

	req, err := c.newRequest(ctx, "GET", manifestURL)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("manifest request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("authentication failed (HTTP %d): please update session cookies in Settings", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest request returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decoding manifest JSON failed: %w", err)
	}
	return &m, nil
}

// FetchTOC retrieves the Table of Contents tree for canonical reading sequence.
func (c *Client) FetchTOC(ctx context.Context, bookID string) ([]TOCItem, error) {
	cleanID := strings.TrimSuffix(bookID, "VE")
	tocURL := fmt.Sprintf("https://learning.oreilly.com/api/v2/epubs/urn:orm:book:%s/table-of-contents/", cleanID)

	req, err := c.newRequest(ctx, "GET", tocURL)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TOC endpoint returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var items []TOCItem
	if err := json.Unmarshal(body, &items); err == nil {
		return items, nil
	}

	var wrapper struct {
		Results []TOCItem `json:"results"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil {
		return wrapper.Results, nil
	}

	return nil, nil
}

// FetchAllFiles iterates through pagination to retrieve every file in the book.
func (c *Client) FetchAllFiles(ctx context.Context, filesEndpoint string) ([]FileInfo, error) {
	if strings.HasPrefix(filesEndpoint, "/") {
		filesEndpoint = "https://learning.oreilly.com" + filesEndpoint
	}
	delimiter := "?"
	if strings.Contains(filesEndpoint, "?") {
		delimiter = "&"
	}
	nextURL := filesEndpoint + delimiter + "limit=100"

	var allFiles []FileInfo

	for nextURL != "" {
		if strings.HasPrefix(nextURL, "/") {
			nextURL = "https://learning.oreilly.com" + nextURL
		}

		req, err := c.newRequest(ctx, "GET", nextURL)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("files list request failed: %w", err)
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			return nil, fmt.Errorf("authentication failed (HTTP %d): please update session cookies in Settings", resp.StatusCode)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("files list request returned HTTP %d", resp.StatusCode)
		}

		var page FilesPageResponse
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decoding files page failed: %w", err)
		}

		allFiles = append(allFiles, page.Results...)

		if page.Next != nil && *page.Next != "" {
			nextURL = *page.Next
		} else {
			nextURL = ""
		}
	}

	return allFiles, nil
}

// DownloadBlob downloads raw file content with retries on transient errors.
func (c *Client) DownloadBlob(ctx context.Context, url string, maxRetries int) ([]byte, error) {
	if strings.HasPrefix(url, "/") {
		url = "https://learning.oreilly.com" + url
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := c.newRequest(ctx, "GET", url)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt*300) * time.Millisecond):
				continue
			}
		}

		if resp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			return data, err
		}

		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("authentication error (HTTP %d): please update cookies in Settings", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("file not found (404)")
		}

		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt*400) * time.Millisecond):
		}
	}

	return nil, lastErr
}
