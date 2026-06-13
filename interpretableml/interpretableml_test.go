package interpretableml_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tamnd/interpretableml-cli/interpretableml"
)

// minimalHTML is a trimmed replica of the real Quarto sidebar structure.
const minimalHTML = `<!DOCTYPE html>
<html>
<body>
<nav id="quarto-sidebar">
  <div>
  <a href="./index.html" class="sidebar-item-text sidebar-link active">
 <span class="menu-text">About the Book</span></a>
  </div>
  <div>
  <a href="./intro.html" class="sidebar-item-text sidebar-link">
 <span class="menu-text"><span class="chapter-number">1</span>&nbsp; <span class="chapter-title">Introduction</span></span></a>
  </div>
  <div>
  <a href="./interpretability.html" class="sidebar-item-text sidebar-link">
 <span class="menu-text"><span class="chapter-number">2</span>&nbsp; <span class="chapter-title">Interpretability</span></span></a>
  </div>
  <div>
            <a class="sidebar-item-text sidebar-link text-start" data-bs-toggle="collapse">
 <span class="menu-text">Interpretable Models</span></a>
  </div>
  <div>
  <a href="./limo.html" class="sidebar-item-text sidebar-link">
 <span class="menu-text"><span class="chapter-number">6</span>&nbsp; <span class="chapter-title">Linear Regression</span></span></a>
  </div>
  <div>
  <a href="./logistic.html" class="sidebar-item-text sidebar-link">
 <span class="menu-text"><span class="chapter-number">7</span>&nbsp; <span class="chapter-title">Logistic Regression</span></span></a>
  </div>
  <div>
            <a class="sidebar-item-text sidebar-link text-start" data-bs-toggle="collapse">
 <span class="menu-text">Model-Agnostic Methods</span></a>
  </div>
  <div>
  <a href="./pdp.html" class="sidebar-item-text sidebar-link">
 <span class="menu-text"><span class="chapter-number">19</span>&nbsp; <span class="chapter-title">Partial Dependence Plot (PDP)</span></span></a>
  </div>
</nav>
</body>
</html>`

func newTestServer(t *testing.T, body string) (*httptest.Server, *interpretableml.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	cfg := interpretableml.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	c := interpretableml.NewClient(cfg)
	return srv, c
}

func TestContents(t *testing.T) {
	_, c := newTestServer(t, minimalHTML)

	chapters, err := c.Contents(context.Background(), 0)
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	// About the Book, Introduction, Interpretability, Linear Regression,
	// Logistic Regression, Partial Dependence Plot
	if len(chapters) != 6 {
		t.Fatalf("got %d chapters, want 6; titles: %v", len(chapters), titlesOf(chapters))
	}
}

func TestContentsLimit(t *testing.T) {
	_, c := newTestServer(t, minimalHTML)

	chapters, err := c.Contents(context.Background(), 3)
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if len(chapters) != 3 {
		t.Fatalf("got %d chapters with limit=3, want 3", len(chapters))
	}
}

func TestContentsPartAssignment(t *testing.T) {
	_, c := newTestServer(t, minimalHTML)

	chapters, err := c.Contents(context.Background(), 0)
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}

	for _, ch := range chapters {
		switch ch.Title {
		case "About the Book", "Introduction", "Interpretability":
			if ch.Part != "Frontmatter" {
				t.Errorf("%q part = %q, want Frontmatter", ch.Title, ch.Part)
			}
		case "Linear Regression", "Logistic Regression":
			if ch.Part != "Interpretable Models" {
				t.Errorf("%q part = %q, want Interpretable Models", ch.Title, ch.Part)
			}
		case "Partial Dependence Plot (PDP)":
			if ch.Part != "Model-Agnostic Methods" {
				t.Errorf("%q part = %q, want Model-Agnostic Methods", ch.Title, ch.Part)
			}
		}
	}
}

func TestContentsChapterNumbers(t *testing.T) {
	_, c := newTestServer(t, minimalHTML)

	chapters, err := c.Contents(context.Background(), 0)
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}

	for _, ch := range chapters {
		switch ch.Title {
		case "Introduction":
			if ch.Number != "1" {
				t.Errorf("Introduction number = %q, want 1", ch.Number)
			}
		case "Linear Regression":
			if ch.Number != "6" {
				t.Errorf("Linear Regression number = %q, want 6", ch.Number)
			}
		}
	}
}

func TestContentsURLs(t *testing.T) {
	srv, c := newTestServer(t, minimalHTML)

	chapters, err := c.Contents(context.Background(), 0)
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	for _, ch := range chapters {
		if !strings.HasPrefix(ch.URL, srv.URL) {
			t.Errorf("chapter %q URL %q does not start with base URL %q", ch.Title, ch.URL, srv.URL)
		}
		if !strings.HasSuffix(ch.URL, ".html") {
			t.Errorf("chapter %q URL %q does not end with .html", ch.Title, ch.URL)
		}
	}
}

func TestSearch(t *testing.T) {
	_, c := newTestServer(t, minimalHTML)

	hits, err := c.Search(context.Background(), "regression", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("Search(regression): got %d hits, want 2; titles: %v", len(hits), titlesOf(hits))
	}
}

func TestSearchLimit(t *testing.T) {
	_, c := newTestServer(t, minimalHTML)

	hits, err := c.Search(context.Background(), "a", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) > 2 {
		t.Errorf("Search with limit=2 returned %d hits", len(hits))
	}
}

func TestSearchNoResults(t *testing.T) {
	_, c := newTestServer(t, minimalHTML)

	hits, err := c.Search(context.Background(), "zzznomatch", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits for no-match query, got %d", len(hits))
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	cfg := interpretableml.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := interpretableml.NewClient(cfg)

	body, err := c.Get(context.Background(), srv.URL+"/test")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
}

func TestGetUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		if ua == "" {
			t.Error("request carried no User-Agent")
		}
		if !strings.Contains(ua, "iml") {
			t.Errorf("User-Agent %q does not contain iml", ua)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := interpretableml.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	c := interpretableml.NewClient(cfg)

	_, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
}

func titlesOf(chapters []interpretableml.Chapter) []string {
	out := make([]string, len(chapters))
	for i, ch := range chapters {
		out[i] = ch.Title
	}
	return out
}
