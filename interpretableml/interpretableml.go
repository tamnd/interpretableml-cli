// Package interpretableml is the library behind the iml command line:
// the HTTP client, request shaping, and the typed data models for the
// Interpretable Machine Learning book at
// https://christophm.github.io/interpretable-ml-book/.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public site throws under load.
package interpretableml

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultUserAgent identifies the client to the book site. A real, honest
// User-Agent is both polite and the thing most likely to keep you unblocked.
const DefaultUserAgent = "iml/dev (+https://github.com/tamnd/interpretableml-cli)"

// ErrNotFound is returned when a resource cannot be found.
var ErrNotFound = errors.New("not found")

// Config holds constructor parameters.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   "https://christophm.github.io/interpretable-ml-book",
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Retries:   5,
		Timeout:   30 * time.Second,
	}
}

// Chapter is a single entry in the Interpretable ML book table of contents.
type Chapter struct {
	Number string `json:"number"`
	Title  string `json:"title"`
	Part   string `json:"part"`
	URL    string `json:"url"`
}

// Client talks to the Interpretable ML book site over HTTP.
type Client struct {
	httpClient *http.Client
	baseURL    string
	userAgent  string
	rate       time.Duration
	retries    int
	mu         sync.Mutex
	last       time.Time
}

// NewClient returns a Client with the given config.
func NewClient(cfg Config) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: cfg.Timeout},
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		userAgent:  cfg.UserAgent,
		rate:       cfg.Rate,
		retries:    cfg.Retries,
	}
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rate <= 0 {
		return
	}
	if wait := c.rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// Contents fetches the main page and returns all chapters in order.
// It uses stdlib strings only — no x/net/html.
// limit <= 0 returns all chapters.
func (c *Client) Contents(ctx context.Context, limit int) ([]Chapter, error) {
	body, err := c.Get(ctx, c.baseURL+"/")
	if err != nil {
		return nil, fmt.Errorf("contents: %w", err)
	}
	chapters := parseContents(string(body), c.baseURL)
	if limit > 0 && limit < len(chapters) {
		chapters = chapters[:limit]
	}
	return chapters, nil
}

// parseContents extracts chapters from the Interpretable ML book homepage HTML.
//
// The page uses a Quarto sidebar with two patterns, where the <a> tag and the
// <span class="menu-text"> may span multiple lines:
//
// Part headings (no chapter-number):
//
//	<a class="sidebar-item-text sidebar-link text-start" ...>
//	 <span class="menu-text">Interpretable Models</span></a>
//
// Chapter links (with chapter-number):
//
//	<a href="./limo.html" class="sidebar-item-text sidebar-link">
//	 <span class="menu-text"><span class="chapter-number">6</span>&nbsp; <span class="chapter-title">Linear Regression</span></span></a>
//
// We split on the anchor tag marker and process each block:
// - blocks containing "text-start" without "chapter-number" are part headings
// - blocks containing "chapter-number" are chapter entries
// - blocks with href but no chapter-number are frontmatter entries (e.g. About the Book)
func parseContents(html, baseURL string) []Chapter {
	var chapters []Chapter
	currentPart := "Frontmatter"

	// Split on <a so each element starts at an anchor tag.
	// The first split element is the preamble before any <a; skip it.
	parts := strings.Split(html, "<a ")
	for _, block := range parts[1:] {
		if !strings.Contains(block, "sidebar-item-text sidebar-link") {
			continue
		}

		// Part heading: text-start and no chapter-number
		if strings.Contains(block, "text-start") && !strings.Contains(block, "chapter-number") {
			part := extractSpanText(block, "menu-text")
			if part != "" {
				currentPart = part
			}
			continue
		}

		href := extractAttr(block, "href")
		if href == "" {
			continue
		}

		// Chapter entry with number and title
		if strings.Contains(block, "chapter-number") {
			num := extractSpanText(block, "chapter-number")
			title := extractSpanText(block, "chapter-title")
			if title == "" {
				continue
			}
			url := resolveURL(baseURL, href)
			chapters = append(chapters, Chapter{
				Number: num,
				Title:  title,
				Part:   currentPart,
				URL:    url,
			})
			continue
		}

		// Frontmatter entry (e.g. "About the Book") — no chapter-number
		title := extractSpanText(block, "menu-text")
		if title == "" {
			continue
		}
		url := resolveURL(baseURL, href)
		chapters = append(chapters, Chapter{
			Number: "",
			Title:  title,
			Part:   currentPart,
			URL:    url,
		})
	}
	return chapters
}

// resolveURL turns a relative href like "./limo.html" into an absolute URL.
func resolveURL(baseURL, href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	href = strings.TrimPrefix(href, "./")
	return baseURL + "/" + href
}

// Search filters Contents by query string (case-insensitive title or part match).
// limit <= 0 returns all matching chapters.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Chapter, error) {
	all, err := c.Contents(ctx, 0)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var out []Chapter
	for _, ch := range all {
		if strings.Contains(strings.ToLower(ch.Title), q) ||
			strings.Contains(strings.ToLower(ch.Part), q) ||
			strings.Contains(strings.ToLower(ch.Number), q) {
			out = append(out, ch)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// extractAttr returns the value of the first occurrence of attr="..." in s.
func extractAttr(s, attr string) string {
	needle := attr + `="`
	i := strings.Index(s, needle)
	if i < 0 {
		return ""
	}
	s = s[i+len(needle):]
	j := strings.Index(s, `"`)
	if j < 0 {
		return ""
	}
	return s[:j]
}

// extractSpanText returns the text content of the first <span class="cls">...</span> in s.
// It handles the case where the span contains nested spans (stripping inner tags).
func extractSpanText(s, cls string) string {
	needle := `class="` + cls + `"`
	i := strings.Index(s, needle)
	if i < 0 {
		return ""
	}
	// Find the closing > of the opening tag
	j := strings.Index(s[i:], ">")
	if j < 0 {
		return ""
	}
	inner := s[i+j+1:]
	end := strings.Index(inner, "</span>")
	if end < 0 {
		return ""
	}
	raw := inner[:end]
	// Strip any nested HTML tags
	raw = stripTags(raw)
	// Decode &nbsp; entities
	raw = strings.ReplaceAll(raw, "&nbsp;", " ")
	return strings.TrimSpace(raw)
}

// stripTags removes all HTML tags from s, leaving only text nodes.
func stripTags(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "<")
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		j := strings.Index(s[i:], ">")
		if j < 0 {
			break
		}
		s = s[i+j+1:]
	}
	return b.String()
}
