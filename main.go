package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/geziyor/geziyor"
	"github.com/geziyor/geziyor/client"
)

// Data models for JSON response

type Photo struct {
	ID        string `json:"id"`
	PageURL   string `json:"page_url,omitempty"`
	Image1500 string `json:"image_1500"`
}

// zoneramaAlbumHandler parses a single album only when the link contains "/Album/".
func zoneramaAlbumHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	link := r.URL.Query().Get("link")
	if link == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing link param: /zonerama-album?link=https://eu.zonerama.com/<Account>/Album/<AlbumId>"})
		return
	}
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid link URL"})
		return
	}
	if !strings.Contains(u.Host, "zonerama.com") {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "link must point to zonerama.com"})
		return
	}
	if !strings.Contains(u.Path, "/Album/") {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "zonerama-album expects an album link containing /Album/"})
		return
	}

	// Limits
	photoLimit := 10 // default 10; 0 = no limit
	if s := r.URL.Query().Get("photo_limit"); s != "" {
		fmt.Sscanf(s, "%d", &photoLimit)
	}
	debugSave := false
	if s := r.URL.Query().Get("debug"); s != "" {
		if b, err := strconv.ParseBool(s); err == nil {
			debugSave = b
		}
	}

	// Rendering toggle: default true, can be disabled with rendered=false or no-render/no_render=true
	useRendered := true
	if s := r.URL.Query().Get("rendered"); s != "" {
		if b, err := strconv.ParseBool(s); err == nil {
			useRendered = b
		}
	}
	if s := r.URL.Query().Get("no-render"); s != "" {
		if b, err := strconv.ParseBool(s); err == nil && b {
			useRendered = false
		}
	}
	if s := r.URL.Query().Get("no_render"); s != "" {
		if b, err := strconv.ParseBool(s); err == nil && b {
			useRendered = false
		}
	}
	doGet := func(g *geziyor.Geziyor, u string, cb func(*geziyor.Geziyor, *client.Response)) {
		if useRendered {
			g.GetRendered(u, cb)
		} else {
			g.Get(u, cb)
		}
	}

	// Response accumulator
	var (
		resp Response
		mu   sync.Mutex
	)
	resp.InputLink = link

	// Regexes
	photoIDRe := regexp.MustCompile(`(?i)^\d+$`)
	rePhotoIDFromHref := regexp.MustCompile(`/Photo/\d+/(\d+)`)
	rePhotoIDFromImg := regexp.MustCompile(`/photos/(\d+)_`)

	// Debug helpers
	sanitizeRe := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	saveDebug := func(stage string, cr *client.Response) {
		if !debugSave || cr == nil || cr.Request == nil {
			return
		}
		_ = os.MkdirAll("debuging", 0o755)
		u := cr.Request.URL.String()
		h := sha1.Sum([]byte(u))
		short := hex.EncodeToString(h[:6])
		name := fmt.Sprintf("%s_%s_%s.html", stage, short, sanitizeRe.ReplaceAllString(u, "_"))
		path := filepath.Join("debuging", name)
		_ = os.WriteFile(path, cr.Body, 0o644)
	}

	// Parse a single album
	parseAlbum := func(g *geziyor.Geziyor, cr *client.Response) {
		saveDebug("album", cr)
		doc := cr.HTMLDoc
		if doc == nil {
			return
		}
		album := Album{URL: cr.Request.URL.String()}
		if sel := doc.Find("meta[property='znrm:album']"); sel.Length() > 0 {
			if v, ok := sel.Attr("content"); ok {
				album.ID = strings.TrimSpace(v)
			}
		}
		album.Title = strings.TrimSpace(doc.Find(".row-name-album h2 span").First().Text())
		dt := strings.TrimSpace(doc.Find(".row-name-album .album-info .hide-on-phone").First().Text())
		if dt != "" {
			dt = strings.TrimSpace(strings.TrimPrefix(dt, "|"))
		}
		album.Date = dt
		if pc := strings.TrimSpace(doc.Find(".row-name-album [data-id='header-album-photos']").First().Text()); pc != "" {
			fmt.Sscanf(pc, "%d", &album.PhotosCnt)
		}
		// Photos
		count := 0
		photoSel := doc.Find("[data-type='photo'][data-id]")
		if photoSel.Length() == 0 {
			photoSel = doc.Find(".gallery-inner [data-id]")
		}
		photoSel.Each(func(i int, s *goquery.Selection) {
			if photoLimit > 0 && count >= photoLimit {
				return
			}
			pid := strings.TrimSpace(s.AttrOr("data-id", ""))
			if pid == "" || !photoIDRe.MatchString(pid) {
				return
			}
			p := Photo{ID: pid}
			if a := s.Find("a.gallery-link"); a.Length() > 0 {
				p.PageURL = a.AttrOr("href", "")
				if strings.HasPrefix(p.PageURL, "/") {
					p.PageURL = cr.JoinURL(p.PageURL)
				}
			}
			p.Image1500 = fmt.Sprintf("https://%s/photos/%s_1500x1000.jpg", cr.Request.URL.Host, pid)
			album.Photos = append(album.Photos, p)
			count++
		})
		if count == 0 {
			doc.Find("a[href*='/Photo/']").Each(func(i int, a *goquery.Selection) {
				if photoLimit > 0 && count >= photoLimit {
					return
				}
				href := strings.TrimSpace(a.AttrOr("href", ""))
				if href == "" {
					return
				}
				m := rePhotoIDFromHref.FindStringSubmatch(href)
				if len(m) < 2 {
					return
				}
				pid := m[1]
				if !photoIDRe.MatchString(pid) {
					return
				}
				p := Photo{ID: pid, PageURL: href}
				if strings.HasPrefix(p.PageURL, "/") {
					p.PageURL = cr.JoinURL(p.PageURL)
				}
				p.Image1500 = fmt.Sprintf("https://%s/photos/%s_1500x1000.jpg", cr.Request.URL.Host, pid)
				album.Photos = append(album.Photos, p)
				count++
			})
		}
		if count == 0 {
			doc.Find("img[src*='/photos/']").Each(func(i int, img *goquery.Selection) {
				if photoLimit > 0 && count >= photoLimit {
					return
				}
				src := strings.TrimSpace(img.AttrOr("src", ""))
				m := rePhotoIDFromImg.FindStringSubmatch(src)
				if len(m) < 2 {
					return
				}
				pid := m[1]
				p := Photo{ID: pid}
				p.Image1500 = fmt.Sprintf("https://%s/photos/%s_1500x1000.jpg", cr.Request.URL.Host, pid)
				album.Photos = append(album.Photos, p)
				count++
			})
		}
		mu.Lock()
		resp.Albums = append(resp.Albums, album)
		mu.Unlock()
	}

	gz := geziyor.NewGeziyor(&geziyor.Options{
		StartRequestsFunc: func(g *geziyor.Geziyor) {
			doGet(g, link, parseAlbum)
		},
		ParseFunc:         parseAlbum,
		RetryTimes:        2,
		Timeout:           30 * time.Second,
		LogDisabled:       true,
		RobotsTxtDisabled: true,
	})
	gz.Start()

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}

type Album struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Date      string  `json:"date,omitempty"`
	PhotosCnt int     `json:"photos_count,omitempty"`
	ViewsCnt  int     `json:"views_count,omitempty"`
	Photos    []Photo `json:"photos"`
}

type Response struct {
	InputLink string  `json:"input_link"`
	Albums    []Album `json:"albums"`
}

func main() {
	http.HandleFunc("/zonerama", zoneramaHandler)
	http.HandleFunc("/zonerama-album", zoneramaAlbumHandler)
	http.HandleFunc("/", docsHandler)
	// Serve saved debug HTML files
	http.Handle("/debuging/", http.StripPrefix("/debuging/", http.FileServer(http.Dir("debuging"))))
	addr := ":7053"
	log.Printf("Starting server on %s...", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

// docsHandler serves a minimal API docs page at "/"
func docsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>ZoneramaScraper API</title>
  <style>
    body { font-family: -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif; margin: 2rem; line-height: 1.5; }
    code, pre { background: #f6f8fa; padding: 2px 6px; border-radius: 4px; }
    pre { padding: 12px; overflow-x: auto; }
    h1 { margin-top: 0; }
    a { color: #0366d6; text-decoration: none; }
    a:hover { text-decoration: underline; }
    .endpoint { border-left: 4px solid #28a745; padding-left: 10px; margin: 1.5rem 0; }
  </style>
</head>
<body>
  <h1>ZoneramaScraper API</h1>
  <p>This service scrapes album and photo metadata from <a href="https://www.zonerama.com" target="_blank" rel="noreferrer">zonerama.com</a>.</p>
  <div class="endpoint">
    <h2>GET /zonerama</h2>
    <p>Scrape albums and photos from a Zonerama profile or album URL.</p>
    <h3>Query parameters</h3>
    <ul>
      <li><strong>link</strong> (required): A Zonerama URL. Example: <code>https://eu.zonerama.com/&lt;Account&gt;/&lt;TabId&gt;</code> or a profile link.</li>
      <li><strong>album_limit</strong> (optional): Integer to limit number of albums processed from a profile. Default: <code>5</code>. <code>0</code> means no limit.</li>
      <li><strong>photo_limit</strong> (optional): Integer to limit number of photos scraped per album. Default: <code>10</code>. <code>0</code> means no limit.</li>
      <li><strong>debug</strong> (optional): <code>true|false</code>. If <code>true</code>, saves fetched HTML files into <code>debuging/</code> and serves them at <code>/debuging/</code>.</li>
    </ul>
    <h3>Example</h3>
    <p><code>/zonerama?link=https://eu.zonerama.com/SomeAccount/12345&amp;album_limit=5&amp;photo_limit=50</code></p>
    <h3>Response</h3>
    <pre>{
  "input_link": "...",
  "albums": [
    {
      "id": "...",
      "title": "...",
      "url": "...",
      "date": "...",
      "photos_count": 42,
      "photos": [
        { "id": "...", "page_url": "...", "image_1500": "..." }
      ]
    }
  ]
}</pre>
  </div>
  <div class="endpoint">
    <h2>GET /zonerama-album</h2>
    <p>Scrape a single album by URL (link must contain <code>/Album/</code>).</p>
    <h3>Query parameters</h3>
    <ul>
      <li><strong>link</strong> (required): A Zonerama album URL. Example: <code>https://eu.zonerama.com/&lt;Account&gt;/Album/&lt;AlbumId&gt;</code>.</li>
      <li><strong>photo_limit</strong> (optional): Integer to limit number of photos scraped from the album. Default: <code>10</code>. <code>0</code> means no limit.</li>
      <li><strong>rendered</strong> (optional): <code>true|false</code>. Default: <code>true</code>. Aliases: <code>no-render=true</code> or <code>no_render=true</code> to disable rendering.</li>
      <li><strong>debug</strong> (optional): <code>true|false</code>. If <code>true</code>, saves fetched HTML files into <code>debuging/</code> and serves them at <code>/debuging/</code>.</li>
    </ul>
    <h3>Example</h3>
    <p><code>/zonerama-album?link=https://eu.zonerama.com/Fcbizoni/Album/13878599&amp;photo_limit=25</code></p>
  </div>
  <p>Server listens on <code>:7053</code>. CORS is enabled allowing all origins (<code>Access-Control-Allow-Origin: *</code>).</p>
  <p>JS rendering is enabled by default (requires Chrome installed). When <code>debug=true</code>, fetched pages are saved beneath <code>debuging/</code> and can be viewed at <code>/debuging/</code>.</p>
</body>
</html>`)
}

func zoneramaHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	link := r.URL.Query().Get("link")
	if link == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing link param: /zonerama?link=https://eu.zonerama.com/<Account>/<TabId> or Profile link"})
		return
	}

	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid link URL"})
		return
	}
	// Basic guard to keep scope on zonerama
	if !strings.Contains(u.Host, "zonerama.com") {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "link must point to zonerama.com"})
		return
	}
	albumLimit := 5 // default 5; 0 = no limit
	if s := r.URL.Query().Get("album_limit"); s != "" {
		fmt.Sscanf(s, "%d", &albumLimit)
	}
	// Optional: limit number of photos per album
	photoLimit := 10 // default 10; 0 = no limit
	if s := r.URL.Query().Get("photo_limit"); s != "" {
		fmt.Sscanf(s, "%d", &photoLimit)
	}
	// JS-rendered requests are used by default (no 'rendered' query param)
	// Optional: enable debug HTML saving
	debugSave := false
	if s := r.URL.Query().Get("debug"); s != "" {
		if b, err := strconv.ParseBool(s); err == nil {
			debugSave = b
		}
	}

	// Rendering toggle: default true; can disable via rendered=false or no-render/no_render=true
	useRendered := true
	if s := r.URL.Query().Get("rendered"); s != "" {
		if b, err := strconv.ParseBool(s); err == nil {
			useRendered = b
		}
	}
	if s := r.URL.Query().Get("no-render"); s != "" {
		if b, err := strconv.ParseBool(s); err == nil && b {
			useRendered = false
		}
	}
	if s := r.URL.Query().Get("no_render"); s != "" {
		if b, err := strconv.ParseBool(s); err == nil && b {
			useRendered = false
		}
	}
	// Helper to choose between rendered and non-rendered fetch
	doGet := func(g *geziyor.Geziyor, url string, cb func(*geziyor.Geziyor, *client.Response)) {
		if useRendered {
			g.GetRendered(url, cb)
		} else {
			g.Get(url, cb)
		}
	}

	// Concurrency for JS-rendered album requests
	concurrency := 8 // default
	if s := r.URL.Query().Get("concurrency"); s != "" {
		fmt.Sscanf(s, "%d", &concurrency)
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if albumLimit > 0 && concurrency > albumLimit {
		concurrency = albumLimit
	}
	// Semaphore to cap concurrent album fetches
	sem := make(chan struct{}, concurrency)

	// Shared state for the crawl
	var (
		resp Response
		mu   sync.Mutex
		wg   sync.WaitGroup
		seen = make(map[string]bool) // dedupe album URLs
	)
	resp.InputLink = link

	// Compile regexes once
	// In a raw string literal (backticks), use a single backslash for \d
	photoIDRe := regexp.MustCompile(`(?i)^\d+$`)
	// Fallback patterns to extract photo IDs
	rePhotoIDFromHref := regexp.MustCompile(`/Photo/\d+/(\d+)`)
	rePhotoIDFromImg := regexp.MustCompile(`/photos/(\d+)_`)

	// Albums accumulator
	addAlbum := func(a Album) {
		mu.Lock()
		resp.Albums = append(resp.Albums, a)
		mu.Unlock()
	}

	// Prelim info gathered from profile tiles (date, counts) keyed by album URL
	type prelimInfo struct {
		Date      string
		PhotosCnt int
		ViewsCnt  int
	}
	prelim := make(map[string]prelimInfo)

	// Debug helpers
	sanitizeRe := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	saveDebug := func(stage string, cr *client.Response) {
		if !debugSave || cr == nil || cr.Request == nil {
			return
		}
		_ = os.MkdirAll("debuging", 0o755)
		u := cr.Request.URL.String()
		h := sha1.Sum([]byte(u))
		short := hex.EncodeToString(h[:6])
		name := fmt.Sprintf("%s_%s_%s.html", stage, short, sanitizeRe.ReplaceAllString(u, "_"))
		path := filepath.Join("debuging", name)
		_ = os.WriteFile(path, cr.Body, 0o644)
	}

	// Crawl an Album page and collect photos
	parseAlbum := func(g *geziyor.Geziyor, cr *client.Response) {
		saveDebug("album", cr)
		doc := cr.HTMLDoc
		if doc == nil {
			return
		}

		album := Album{URL: cr.Request.URL.String()}

		// ID from meta
		if sel := doc.Find("meta[property='znrm:album']"); sel.Length() > 0 {
			if v, ok := sel.Attr("content"); ok {
				album.ID = strings.TrimSpace(v)
			}
		}
		// Title from header
		album.Title = strings.TrimSpace(doc.Find(".row-name-album h2 span").First().Text())
		// Date (normalize to drop leading '|' if present)
		dateText := strings.TrimSpace(doc.Find(".row-name-album .album-info .hide-on-phone").First().Text())
		if dateText != "" {
			dateText = strings.TrimSpace(strings.TrimPrefix(dateText, "|"))
		}
		album.Date = dateText
		// Photos count
		if pc := strings.TrimSpace(doc.Find(".row-name-album [data-id='header-album-photos']").First().Text()); pc != "" {
			fmt.Sscanf(pc, "%d", &album.PhotosCnt)
		}

		// Photos list: support broader selectors
		count := 0
		photoSel := doc.Find("[data-type='photo'][data-id]")
		if photoSel.Length() == 0 {
			// fallback: look inside .gallery-inner for any element with data-id
			photoSel = doc.Find(".gallery-inner [data-id]")
		}
		log.Printf("parseAlbum: found %d photo candidates at %s", photoSel.Length(), cr.Request.URL.String())
		photoSel.Each(func(i int, s *goquery.Selection) {
			if photoLimit > 0 && count >= photoLimit {
				return
			}
			pid := strings.TrimSpace(s.AttrOr("data-id", ""))
			if pid == "" || !photoIDRe.MatchString(pid) {
				return
			}
			p := Photo{ID: pid}
			// Optional page URL
			if a := s.Find("a.gallery-link"); a.Length() > 0 {
				p.PageURL = a.AttrOr("href", "")
				if strings.HasPrefix(p.PageURL, "/") {
					p.PageURL = cr.JoinURL(p.PageURL)
				}
			}
			p.Image1500 = fmt.Sprintf("https://%s/photos/%s_1500x1000.jpg", cr.Request.URL.Host, pid)
			album.Photos = append(album.Photos, p)
			count++
		})

		// If none matched, try fallback: anchors with /Photo/<album>/<photo>
		if count == 0 {
			fallbackCount := 0
			doc.Find("a[href*='/Photo/']").Each(func(i int, a *goquery.Selection) {
				if photoLimit > 0 && count >= photoLimit {
					return
				}
				href := strings.TrimSpace(a.AttrOr("href", ""))
				if href == "" {
					return
				}
				m := rePhotoIDFromHref.FindStringSubmatch(href)
				if len(m) < 2 {
					return
				}
				pid := m[1]
				if !photoIDRe.MatchString(pid) {
					return
				}
				p := Photo{ID: pid, PageURL: href}
				if strings.HasPrefix(p.PageURL, "/") {
					p.PageURL = cr.JoinURL(p.PageURL)
				}
				p.Image1500 = fmt.Sprintf("https://%s/photos/%s_1500x1000.jpg", cr.Request.URL.Host, pid)
				album.Photos = append(album.Photos, p)
				count++
				fallbackCount++
			})
			if fallbackCount > 0 {
				log.Printf("parseAlbum: fallback from anchors found %d photos at %s", fallbackCount, cr.Request.URL.String())
			}
		}

		// If still none, try images with /photos/<id>_
		if count == 0 {
			fallbackCount := 0
			doc.Find("img[src*='/photos/']").Each(func(i int, img *goquery.Selection) {
				if photoLimit > 0 && count >= photoLimit {
					return
				}
				src := strings.TrimSpace(img.AttrOr("src", ""))
				m := rePhotoIDFromImg.FindStringSubmatch(src)
				if len(m) < 2 {
					return
				}
				pid := m[1]
				p := Photo{ID: pid}
				p.Image1500 = fmt.Sprintf("https://%s/photos/%s_1500x1000.jpg", cr.Request.URL.Host, pid)
				album.Photos = append(album.Photos, p)
				count++
				fallbackCount++
			})
			if fallbackCount > 0 {
				log.Printf("parseAlbum: fallback from images found %d photos at %s", fallbackCount, cr.Request.URL.String())
			}
		}

		// Merge prelim info (from profile tiles) if available
		mu.Lock()
		if pi, ok := prelim[album.URL]; ok {
			if strings.TrimSpace(album.Date) == "" && strings.TrimSpace(pi.Date) != "" {
				album.Date = strings.TrimSpace(pi.Date)
			}
			if album.PhotosCnt == 0 && pi.PhotosCnt > 0 {
				album.PhotosCnt = pi.PhotosCnt
			}
			if album.ViewsCnt == 0 && pi.ViewsCnt > 0 {
				album.ViewsCnt = pi.ViewsCnt
			}
		}
		mu.Unlock()

		addAlbum(album)
	}
	parseProfile := func(g *geziyor.Geziyor, cr *client.Response) {
		saveDebug("profile", cr)
		doc := cr.HTMLDoc
		if doc == nil {
			return
		}
		// Albums tiles: try multiple selectors
		count := 0
		albumTiles := doc.Find("li.list-alb")
		if albumTiles.Length() == 0 {
			albumTiles = doc.Find("[data-type='album'], li[class*='list-alb']")
		}
		log.Printf("parseProfile: found %d album candidates at %s", albumTiles.Length(), cr.Request.URL.String())
		// First collect entries with prelim info
		type entry struct {
			URL  string
			Info prelimInfo
		}
		var entries []entry
		albumTiles.Each(func(i int, s *goquery.Selection) {
			albumURL := strings.TrimSpace(s.AttrOr("data-url", ""))
			if albumURL == "" {
				// fallback to anchor
				albumURL = strings.TrimSpace(s.Find("a.thumbnail").AttrOr("href", ""))
				if albumURL == "" {
					// broader fallback: first anchor in tile
					albumURL = strings.TrimSpace(s.Find("a").AttrOr("href", ""))
				}
			}
			if albumURL == "" {
				return
			}
			if strings.HasPrefix(albumURL, "/") {
				albumURL = cr.JoinURL(albumURL)
			}
			// Extract date, photos count and views count from the tile's <p> block
			dateText := ""
			photosCount := 0
			viewsCount := 0
			if p := s.Find("p").First(); p.Length() > 0 {
				full := strings.TrimSpace(p.Text())
				if full != "" {
					parts := strings.Split(full, "|")
					if len(parts) > 0 {
						dateText = strings.TrimSpace(parts[0])
					}
				}
				spans := p.Find("span")
				if spans.Length() > 0 {
					fmt.Sscanf(strings.TrimSpace(spans.Eq(0).Text()), "%d", &photosCount)
				}
				if spans.Length() > 1 {
					fmt.Sscanf(strings.TrimSpace(spans.Eq(1).Text()), "%d", &viewsCount)
				}
			}
			entries = append(entries, entry{URL: albumURL, Info: prelimInfo{Date: dateText, PhotosCnt: photosCount, ViewsCnt: viewsCount}})
		})
		// Sort entries by date descending (newest first)
		parseDate := func(s string) (time.Time, bool) {
			s = strings.TrimSpace(s)
			if s == "" {
				return time.Time{}, false
			}
			layouts := []string{"2. 1. 2006", "2. 1.2006", "2.1.2006", "02.01.2006"}
			for _, layout := range layouts {
				if t, err := time.Parse(layout, s); err == nil {
					return t, true
				}
			}
			return time.Time{}, false
		}
		sort.SliceStable(entries, func(i, j int) bool {
			ti, oi := parseDate(entries[i].Info.Date)
			tj, oj := parseDate(entries[j].Info.Date)
			if oi && oj {
				return ti.After(tj)
			}
			if oi {
				return true
			}
			if oj {
				return false
			}
			return entries[i].URL < entries[j].URL
		})
		// Enqueue requests in sorted order, honoring albumLimit
		for _, e := range entries {
			if albumLimit > 0 && count >= albumLimit {
				break
			}
			// Save prelim info for this album URL
			mu.Lock()
			prelim[e.URL] = e.Info
			if seen[e.URL] {
				mu.Unlock()
				continue
			}
			seen[e.URL] = true
			mu.Unlock()
			count++
			// Acquire a slot before starting the JS-rendered request
			sem <- struct{}{}
			wg.Add(1)
			g.GetRendered(e.URL, func(g2 *geziyor.Geziyor, r2 *client.Response) {
				defer func() { <-sem; wg.Done() }()
				parseAlbum(g2, r2)
			})
		}
	}

	// Router: decide whether current page is a profile or an album and call appropriate parser
	parseRouter := func(g *geziyor.Geziyor, cr *client.Response) {
		saveDebug("router", cr)
		doc := cr.HTMLDoc
		if doc == nil {
			return
		}
		// Heuristics: prefer PROFILE when profile markers exist; ALBUM only with strong markers
		if doc.Find("li.list-alb, #profile-albums").Length() > 0 {
			log.Printf("router: classified as PROFILE -> %s", cr.Request.URL.String())
			parseProfile(g, cr)
			return
		}
		if doc.Find("meta[property='znrm:album']").Length() > 0 || doc.Find(".row-name-album").Length() > 0 {
			log.Printf("router: classified as ALBUM -> %s", cr.Request.URL.String())
			parseAlbum(g, cr)
			return
		}
		// Default to profile for safety
		log.Printf("router: defaulting to PROFILE -> %s", cr.Request.URL.String())
		parseProfile(g, cr)
	}
	gz := geziyor.NewGeziyor(&geziyor.Options{
		StartRequestsFunc: func(g *geziyor.Geziyor) {
			doGet(g, link, parseRouter)
		},
		ParseFunc:         parseRouter,
		RetryTimes:        2,
		Timeout:           30 * time.Second,
		LogDisabled:       true,
		RobotsTxtDisabled: true,
	})

	gz.Start()
	// Wait for all album requests to complete
	wg.Wait()

	// Sort albums by date (descending) using the date format from profile tiles like "20. 9. 2025"
	parseCzDate := func(s string) (time.Time, bool) {
		s = strings.TrimSpace(s)
		if s == "" {
			return time.Time{}, false
		}
		layouts := []string{
			"2. 1. 2006",
			"2. 1.2006",
			"2.1.2006",
			"02.01.2006",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
		return time.Time{}, false
	}
	sort.SliceStable(resp.Albums, func(i, j int) bool {
		ti, oi := parseCzDate(resp.Albums[i].Date)
		tj, oj := parseCzDate(resp.Albums[j].Date)
		if oi && oj {
			return ti.After(tj)
		}
		if oi {
			return true
		}
		if oj {
			return false
		}
		// Fallback: by title
		return resp.Albums[i].Title < resp.Albums[j].Title
	})

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}
