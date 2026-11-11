package manga

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"mangahub/pkg/models"
)

type SeedOptions struct {
	Limit       int
	ScrapeLimit int
	RateLimit   time.Duration
	BaseURL     string
	Language    string
	HTTPTimeout time.Duration
	Logger      *log.Logger
}

type mangaDexResponse struct {
	Result string          `json:"result"`
	Data   []mangaDexManga `json:"data"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
	Total  int             `json:"total"`
}

type mangaDexManga struct {
	ID            string             `json:"id"`
	Attributes    mangaDexAttributes `json:"attributes"`
	Relationships []mangaDexRelation `json:"relationships"`
}

type mangaDexAttributes struct {
	Title       mangaDexLocalizedString `json:"title"`
	AltTitles   []map[string]string     `json:"altTitles"`
	Description mangaDexLocalizedString `json:"description"`
	Status      string                  `json:"status"`
	LastChapter string                  `json:"lastChapter"`
	Tags        []mangaDexTag           `json:"tags"`
}

type mangaDexTag struct {
	Attributes struct {
		Name mangaDexLocalizedString `json:"name"`
	} `json:"attributes"`
}

type mangaDexRelation struct {
	Type       string                 `json:"type"`
	Attributes *mangaDexRelAttributes `json:"attributes,omitempty"`
}

type mangaDexRelAttributes struct {
	Name mangaDexLocalizedString `json:"name"`
}

type mangaDexLocalizedString map[string]string

func (ls *mangaDexLocalizedString) UnmarshalJSON(data []byte) error {
	if ls == nil {
		return nil
	}
	if len(data) == 0 || string(data) == "null" {
		*ls = nil
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if s == "" {
			*ls = nil
			return nil
		}
		*ls = map[string]string{"en": s}
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*ls = m
	return nil
}

// SeedFromMangaDex fetches manga from the MangaDex API, enriches with optional scraping,
// and persists normalized records to SQLite.
func SeedFromMangaDex(ctx context.Context, db *sql.DB, opts SeedOptions) (int, error) {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.RateLimit <= 0 {
		opts.RateLimit = 1 * time.Second
	}
	if opts.BaseURL == "" {
		opts.BaseURL = "https://api.mangadex.org"
	}
	if opts.Language == "" {
		opts.Language = "en"
	}
	if opts.HTTPTimeout <= 0 {
		opts.HTTPTimeout = 20 * time.Second
	}

	existing, err := loadExistingManga(db)
	if err != nil {
		return 0, err
	}

	client := &http.Client{Timeout: opts.HTTPTimeout}
	limiter := time.NewTicker(opts.RateLimit)
	defer limiter.Stop()

	seedItems := make([]seedItem, 0, opts.Limit)
	offset := 0
	for len(seedItems) < opts.Limit {
		pageLimit := opts.Limit - len(seedItems)
		if pageLimit > 100 {
			pageLimit = 100
		}

		if err := waitRate(ctx, limiter.C); err != nil {
			return 0, err
		}
		resp, err := fetchMangaDexPage(ctx, client, opts.BaseURL, opts.Language, pageLimit, offset)
		if err != nil {
			return 0, err
		}
		if resp.Result != "ok" {
			return 0, fmt.Errorf("mangadex result not ok")
		}

		for _, item := range resp.Data {
			normalized := normalizeManga(item, opts.Language)
			if normalized.Title == "" {
				continue
			}
			key := strings.ToLower(normalized.Title + "|" + normalized.Author)
			if _, exists := existing[key]; exists {
				continue
			}
			seedItems = append(seedItems, seedItem{ID: item.ID, Manga: normalized})
			if len(seedItems) >= opts.Limit {
				break
			}
		}

		if len(resp.Data) == 0 {
			break
		}
		offset += resp.Limit
	}

	if opts.ScrapeLimit > 0 {
		for i := 0; i < len(seedItems) && i < opts.ScrapeLimit; i++ {
			if seedItems[i].Manga.Description != "" {
				continue
			}
			if err := waitRate(ctx, limiter.C); err != nil {
				return 0, err
			}
			desc, err := scrapeMangaDexDescription(ctx, client, seedItems[i].ID)
			if err != nil {
				opts.Logger.Printf("scrape error for %s: %v", seedItems[i].ID, err)
				continue
			}
			seedItems[i].Manga.Description = desc
		}
	}

	inserted, err := insertMangaBatch(db, seedItems, existing)
	if err != nil {
		return inserted, err
	}

	opts.Logger.Printf("Seeded %d manga records", inserted)
	return inserted, nil
}

type seedItem struct {
	ID    string
	Manga models.Manga
}

func fetchMangaDexPage(ctx context.Context, client *http.Client, baseURL, language string, limit, offset int) (*mangaDexResponse, error) {
	endpoint := fmt.Sprintf("%s/manga", strings.TrimRight(baseURL, "/"))
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))
	params.Add("includes[]", "author")
	params.Add("includes[]", "artist")
	params.Add("availableTranslatedLanguage[]", language)
	params.Add("contentRating[]", "safe")
	params.Add("order[followedCount]", "desc")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MangaHubSeeder/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mangadex status %d", resp.StatusCode)
	}

	var payload mangaDexResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func normalizeManga(item mangaDexManga, language string) models.Manga {
	title := pickLocalized(map[string]string(item.Attributes.Title), language)
	author := extractAuthor(item.Relationships, language)
	genres := extractTags(item.Attributes.Tags, language)
	description := pickLocalized(map[string]string(item.Attributes.Description), language)

	chapters := 0
	if item.Attributes.LastChapter != "" {
		if v, err := strconv.Atoi(strings.Split(item.Attributes.LastChapter, ".")[0]); err == nil {
			chapters = v
		}
	}

	return models.Manga{
		Title:         title,
		Author:        author,
		Genres:        genres,
		Status:        item.Attributes.Status,
		TotalChapters: chapters,
		Description:   description,
	}
}

func pickLocalized(values map[string]string, language string) string {
	if v, ok := values[language]; ok && v != "" {
		return strings.TrimSpace(v)
	}
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func extractAuthor(relations []mangaDexRelation, language string) string {
	for _, r := range relations {
		if r.Type != "author" || r.Attributes == nil {
			continue
		}
		name := pickLocalized(map[string]string(r.Attributes.Name), language)
		if name != "" {
			return name
		}
	}
	return ""
}

func extractTags(tags []mangaDexTag, language string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		name := pickLocalized(map[string]string(t.Attributes.Name), language)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func loadExistingManga(db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.Query(`SELECT title, author FROM manga`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := make(map[string]struct{})
	for rows.Next() {
		var title, author string
		if err := rows.Scan(&title, &author); err != nil {
			return nil, err
		}
		key := strings.ToLower(title + "|" + author)
		existing[key] = struct{}{}
	}
	return existing, nil
}

func insertMangaBatch(db *sql.DB, items []seedItem, existing map[string]struct{}) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.Prepare(`INSERT INTO manga (title, author, genres, status, total_chapters, description) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for _, item := range items {
		key := strings.ToLower(item.Manga.Title + "|" + item.Manga.Author)
		if _, ok := existing[key]; ok {
			continue
		}
		genresJSON, err := json.Marshal(item.Manga.Genres)
		if err != nil {
			return inserted, err
		}
		_, err = stmt.Exec(item.Manga.Title, item.Manga.Author, string(genresJSON), item.Manga.Status, item.Manga.TotalChapters, item.Manga.Description)
		if err != nil {
			return inserted, err
		}
		existing[key] = struct{}{}
		inserted++
	}

	if err := tx.Commit(); err != nil {
		return inserted, err
	}
	return inserted, nil
}

func scrapeMangaDexDescription(ctx context.Context, client *http.Client, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("missing id")
	}

	pageURL := fmt.Sprintf("https://mangadex.org/title/%s", url.PathEscape(id))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "MangaHubSeeder/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	if desc := findMetaContent(doc, "description"); desc != "" {
		return desc, nil
	}
	if desc := findMetaProperty(doc, "og:description"); desc != "" {
		return desc, nil
	}

	return "", fmt.Errorf("description not found")
}

func findMetaContent(n *html.Node, name string) string {
	var content string
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if content != "" {
			return
		}
		if cur.Type == html.ElementNode && cur.Data == "meta" {
			var metaName, metaContent string
			for _, a := range cur.Attr {
				if a.Key == "name" {
					metaName = a.Val
				}
				if a.Key == "content" {
					metaContent = a.Val
				}
			}
			if strings.EqualFold(metaName, name) && metaContent != "" {
				content = strings.TrimSpace(metaContent)
				return
			}
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return content
}

func findMetaProperty(n *html.Node, property string) string {
	var content string
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if content != "" {
			return
		}
		if cur.Type == html.ElementNode && cur.Data == "meta" {
			var metaProp, metaContent string
			for _, a := range cur.Attr {
				if a.Key == "property" {
					metaProp = a.Val
				}
				if a.Key == "content" {
					metaContent = a.Val
				}
			}
			if strings.EqualFold(metaProp, property) && metaContent != "" {
				content = strings.TrimSpace(metaContent)
				return
			}
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return content
}

func waitRate(ctx context.Context, tick <-chan time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tick:
		return nil
	}
}
