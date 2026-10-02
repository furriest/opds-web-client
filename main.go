package main

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

//go:embed static
var staticFiles embed.FS

// ── Config ────────────────────────────────────────────────────────────────────

type Config struct {
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type ConfigResponse struct {
	URL         string `json:"url"`
	Username    string `json:"username,omitempty"`
	HasPassword bool   `json:"hasPassword"`
}

const cookieName = "opds_cfg"
const cookieMaxAge = 365 * 24 * 60 * 60

// ── OPDS XML structures ───────────────────────────────────────────────────────

type Feed struct {
	XMLName xml.Name `xml:"feed"`
	Title   string   `xml:"title"`
	Links   []Link   `xml:"link"`
	Entries []Entry  `xml:"entry"`
}

type Entry struct {
	Title   string   `xml:"title"`
	ID      string   `xml:"id"`
	Authors []Author `xml:"author"`
	Summary string   `xml:"summary"`
	Content *Content `xml:"content"`
	Links   []Link   `xml:"link"`
}

type Author struct {
	Name string `xml:"name"`
}

type Content struct {
	Text string `xml:",chardata"`
	Type string `xml:"type,attr"`
}

type Link struct {
	Rel   string `xml:"rel,attr"`
	Href  string `xml:"href,attr"`
	Type  string `xml:"type,attr"`
	Title string `xml:"title,attr"`
}

// ── JSON response structures ──────────────────────────────────────────────────

type FeedResponse struct {
	Title     string     `json:"title"`
	Entries   []AnyEntry `json:"entries"`
	Links     NavLinks   `json:"links"`
	SearchURL string     `json:"searchUrl,omitempty"`
}

type AnyEntry struct {
	Kind     string     `json:"kind"` // "book" | "nav"
	Title    string     `json:"title"`
	Authors  []string   `json:"authors,omitempty"`
	Summary  string     `json:"summary,omitempty"`
	NavURL   string     `json:"navUrl,omitempty"`
	CoverURL string     `json:"coverUrl,omitempty"`
	ThumbURL string     `json:"thumbUrl,omitempty"`
	Files    []FileLink `json:"files,omitempty"`
}

type FileLink struct {
	URL    string `json:"url"`
	Type   string `json:"type"`
	Format string `json:"format"`
}

type NavLinks struct {
	Start string `json:"start,omitempty"`
	Up    string `json:"up,omitempty"`
	Next  string `json:"next,omitempty"`
	Prev  string `json:"prev,omitempty"`
}

// ── OpenSearch ────────────────────────────────────────────────────────────────

type OpenSearchDesc struct {
	XMLName xml.Name        `xml:"OpenSearchDescription"`
	URLs    []OpenSearchURL `xml:"Url"`
}

type OpenSearchURL struct {
	Type     string `xml:"type,attr"`
	Template string `xml:"template,attr"`
}

// ── HTTP client ───────────────────────────────────────────────────────────────

var httpClient = newHTTPClient()

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialTLSContext:        dialTLSBrowser,
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   20 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if len(via) > 0 && req.URL.Host == via[0].URL.Host {
				if auth := via[0].Header.Get("Authorization"); auth != "" {
					req.Header.Set("Authorization", auth)
				}
			}
			return nil
		},
	}
}

// dialTLSBrowser creates a TLS connection with a Chrome-like fingerprint.
// Go's http.Transport with DialTLSContext doesn't auto-negotiate HTTP/2, so
// we strip h2 from ALPN to avoid a protocol mismatch when the server
// would otherwise accept h2 but we'd send HTTP/1.1 frames.
func dialTLSBrowser(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	tcp, err := (&net.Dialer{}).DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	cfg := &utls.Config{ServerName: host}

	// Use Chrome fingerprint with http/1.1-only ALPN.
	if spec, specErr := utls.UTLSIdToSpec(utls.HelloChrome_Auto); specErr == nil {
		for i := range spec.Extensions {
			if alpn, ok := spec.Extensions[i].(*utls.ALPNExtension); ok {
				alpn.AlpnProtocols = []string{"http/1.1"}
				break
			}
		}
		conn := utls.UClient(tcp, cfg, utls.HelloCustom)
		if applyErr := conn.ApplyPreset(&spec); applyErr == nil {
			if hsErr := conn.HandshakeContext(ctx); hsErr != nil {
				tcp.Close()
				return nil, hsErr
			}
			return conn, nil
		}
	}

	// Fallback: use Chrome_Auto without ALPN override.
	conn := utls.UClient(tcp, cfg, utls.HelloChrome_Auto)
	if err := conn.HandshakeContext(ctx); err != nil {
		tcp.Close()
		return nil, err
	}
	return conn, nil
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	mux := http.NewServeMux()

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))

	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/api/feed", handleFeed)
	mux.HandleFunc("/api/proxy", handleProxy)
	mux.HandleFunc("/", serveIndex(sub))

	log.Println("Listening on :80")
	if err := http.ListenAndServe(":80", mux); err != nil {
		log.Fatal(err)
	}
}

func serveIndex(sub fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	}
}

// ── Config handler ────────────────────────────────────────────────────────────

func getConfig(r *http.Request) *Config {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return nil
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	return &cfg
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		cfg := getConfig(r)
		if cfg == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(ConfigResponse{
			URL:         cfg.URL,
			Username:    cfg.Username,
			HasPassword: cfg.Password != "",
		})

	case http.MethodPost:
		var cfg Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		cfg.URL = strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
		if cfg.URL == "" {
			http.Error(w, `{"error":"url required"}`, http.StatusBadRequest)
			return
		}
		// Validate URL scheme
		if u, err := url.Parse(cfg.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			http.Error(w, `{"error":"url must start with http:// or https://"}`, http.StatusBadRequest)
			return
		}
		raw, _ := json.Marshal(cfg)
		encoded := base64.RawURLEncoding.EncodeToString(raw)
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    encoded,
			MaxAge:   cookieMaxAge,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		w.WriteHeader(http.StatusNoContent)

	case http.MethodDelete:
		http.SetCookie(w, &http.Cookie{
			Name:   cookieName,
			Value:  "",
			MaxAge: -1,
			Path:   "/",
		})
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ── Feed handler ──────────────────────────────────────────────────────────────

func handleFeed(w http.ResponseWriter, r *http.Request) {
	cfg := getConfig(r)
	if cfg == nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"not configured"}`, http.StatusUnauthorized)
		return
	}

	feedURL := r.URL.Query().Get("url")
	if feedURL == "" {
		feedURL = cfg.URL
	}

	// Validate URL
	if u, err := url.Parse(feedURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		http.Error(w, `{"error":"invalid url"}`, http.StatusBadRequest)
		return
	}

	feed, err := fetchFeed(cfg, feedURL)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	resp := buildResponse(feed, feedURL, cfg)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func fetchFeed(cfg *Config, feedURL string) (*Feed, error) {
	req, err := http.NewRequest("GET", feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/atom+xml, application/xml, text/xml, */*")
	req.Header.Set("User-Agent", "OPDS-Web-Client/1.0")
	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication required (401)")
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("access denied (403)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var feed Feed
	dec := xml.NewDecoder(resp.Body)
	dec.Strict = false
	if err := dec.Decode(&feed); err != nil {
		return nil, fmt.Errorf("XML parse error: %w", err)
	}
	return &feed, nil
}

func buildResponse(feed *Feed, baseURL string, cfg *Config) FeedResponse {
	resp := FeedResponse{Title: feed.Title}

	for _, l := range feed.Links {
		href := resolveURL(baseURL, l.Href)
		switch l.Rel {
		case "start":
			resp.Links.Start = href
		case "up":
			resp.Links.Up = href
		case "next":
			resp.Links.Next = href
		case "previous", "prev":
			resp.Links.Prev = href
		case "search":
			if strings.Contains(l.Type, "opensearchdescription") {
				// Resolve OpenSearch description to get the actual search URL template
				if tmpl := resolveOpenSearch(cfg, href, baseURL); tmpl != "" {
					resp.SearchURL = tmpl
				}
			}
		}
	}

	for _, e := range feed.Entries {
		resp.Entries = append(resp.Entries, buildEntry(e, baseURL))
	}
	return resp
}

func buildEntry(e Entry, baseURL string) AnyEntry {
	ae := AnyEntry{Title: e.Title}

	for _, a := range e.Authors {
		if a.Name != "" {
			ae.Authors = append(ae.Authors, a.Name)
		}
	}

	// Summary: prefer content over summary, strip HTML
	if e.Content != nil && strings.TrimSpace(e.Content.Text) != "" {
		ae.Summary = stripHTML(e.Content.Text)
	} else if e.Summary != "" {
		ae.Summary = stripHTML(e.Summary)
	}

	isBook := false
	var navURL string

	for _, l := range e.Links {
		href := resolveURL(baseURL, l.Href)
		if href == "" {
			continue
		}
		rel := l.Rel
		switch {
		case rel == "http://opds-spec.org/image":
			ae.CoverURL = href
		case rel == "http://opds-spec.org/image/thumbnail":
			ae.ThumbURL = href
		case strings.HasPrefix(rel, "http://opds-spec.org/acquisition"):
			isBook = true
			ae.Files = append(ae.Files, FileLink{
				URL:    href,
				Type:   l.Type,
				Format: mimeToFormat(l.Type),
			})
		case !isBook && (rel == "subsection" || rel == "alternate" || strings.Contains(l.Type, "application/atom+xml")):
			if navURL == "" {
				navURL = href
			}
		}
	}

	if isBook {
		ae.Kind = "book"
	} else {
		ae.Kind = "nav"
		ae.NavURL = navURL
		// Fallback: first link
		if ae.NavURL == "" {
			for _, l := range e.Links {
				if href := resolveURL(baseURL, l.Href); href != "" {
					ae.NavURL = href
					break
				}
			}
		}
	}
	return ae
}

// resolveOpenSearch fetches the OpenSearch description and returns the search URL template.
func resolveOpenSearch(cfg *Config, descURL, baseURL string) string {
	req, err := http.NewRequest("GET", descURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "OPDS-Web-Client/1.0")
	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()

	var desc OpenSearchDesc
	if err := xml.NewDecoder(resp.Body).Decode(&desc); err != nil {
		return ""
	}

	for _, u := range desc.URLs {
		if strings.Contains(u.Type, "atom+xml") || strings.Contains(u.Type, "opds") {
			return resolveURL(baseURL, u.Template)
		}
	}
	for _, u := range desc.URLs {
		if u.Template != "" {
			return resolveURL(baseURL, u.Template)
		}
	}
	return ""
}

// ── Proxy handler ─────────────────────────────────────────────────────────────

func handleProxy(w http.ResponseWriter, r *http.Request) {
	cfg := getConfig(r)
	if cfg == nil {
		http.Error(w, "not configured", http.StatusUnauthorized)
		return
	}

	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}

	u, err := url.Parse(targetURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "OPDS-Web-Client/1.0")
	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}
	// Pass Range header for resume support
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Forward relevant headers
	for _, h := range []string{
		"Content-Type", "Content-Length", "Content-Disposition",
		"Content-Range", "Accept-Ranges", "Last-Modified", "ETag",
	} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	// Ensure images are cached by browser
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "image/") {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func resolveURL(base, ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func mimeToFormat(mime string) string {
	mime = strings.ToLower(mime)
	// Strip parameters like ;type=... or ;profile=...
	if i := strings.Index(mime, ";"); i != -1 {
		mime = strings.TrimSpace(mime[:i])
	}
	switch mime {
	case "application/epub+zip":
		return "EPUB"
	case "application/fb2+zip", "application/x-fictionbook+xml", "application/fb2", "text/fb2+xml":
		return "FB2"
	case "application/x-mobipocket-ebook":
		return "MOBI"
	case "application/pdf":
		return "PDF"
	case "application/djvu", "image/vnd.djvu":
		return "DJVU"
	case "application/zip":
		return "ZIP"
	case "application/x-cbz", "application/vnd.comicbook+zip":
		return "CBZ"
	case "application/x-cbr":
		return "CBR"
	case "text/plain":
		return "TXT"
	default:
		parts := strings.Split(mime, "/")
		if len(parts) == 2 {
			ext := strings.TrimPrefix(parts[1], "x-")
			ext = strings.TrimPrefix(ext, "vnd.")
			return strings.ToUpper(ext)
		}
		return "FILE"
	}
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteRune(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	// Collapse multiple spaces
	result := strings.Join(strings.Fields(b.String()), " ")
	return result
}
