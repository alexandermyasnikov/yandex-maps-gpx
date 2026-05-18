package converter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
	"github.com/tidwall/gjson"
)

const (
	cacheFileName = "yandex-maps-gpx-cache.json"

	pauseAfterRequest = 1 * time.Second

	cacheExpiration = 7 * 24 * time.Hour
)

type (
	Config struct {
		ApiKey    string
		PublicId  string
		OutputDir string
		CacheDir  string
	}

	Converter struct {
		cfg        Config
		cache      Cache
		httpClient *http.Client
	}

	Cache struct {
		Uri  map[string]CacheItem
		Html map[string]CacheItem
	}

	CacheItem struct {
		Updated time.Time
		Data    []byte
	}
)

func (c *Config) Validate() error {
	if c.ApiKey == "" {
		return fmt.Errorf("api_key is required")
	}

	if c.PublicId == "" {
		return fmt.Errorf("public_ids is required")
	}

	if c.OutputDir == "" {
		return fmt.Errorf("output_dir is required")
	}

	if c.CacheDir == "" {
		return fmt.Errorf("cache_dir is required")
	}

	return nil
}

func NewConverter(cfg Config) (*Converter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cookieJar, err := cookiejar.New(&cookiejar.Options{})
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{
		Jar: cookieJar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			slog.Debug("CheckRedirect", "url", req.URL.String())

			u, err := url.Parse(req.URL.String())
			if err != nil {
				return err
			}

			if u.Path == "/showcaptcha" {
				return fmt.Errorf("captcha required")
			}

			return nil
		},
	}

	return &Converter{
		cfg: cfg,
		cache: Cache{
			Uri:  make(map[string]CacheItem),
			Html: make(map[string]CacheItem),
		},
		httpClient: httpClient,
	}, nil
}

func (c *Converter) Convert(ctx context.Context) error {
	slog.Info("convert bookmark", "public_id", c.cfg.PublicId)

	if err := c.readCache(); err != nil {
		return err
	}

	defer func() {
		if err := c.writeCache(); err != nil {
			slog.Error("write cache", "error", err)
		}
	}()

	if err := c.convert(ctx); err != nil {
		return err
	}

	return nil
}

func (c *Converter) readCache() error {
	cachePath := filepath.Join(c.cfg.CacheDir, cacheFileName)

	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("cache file not found, starting fresh", "path", cachePath)
			return nil
		}

		return fmt.Errorf("read cache file: %w", err)
	}

	if err := json.Unmarshal(data, &c.cache); err != nil {
		return fmt.Errorf("parse cache json: %w", err)
	}

	slog.Info("cache loaded",
		"path", cachePath,
		"uri_count", len(c.cache.Uri),
		"html_count", len(c.cache.Html),
	)

	return nil
}

func (c *Converter) writeCache() error {
	data, err := json.MarshalIndent(c.cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache to json: %w", err)
	}

	cachePath := filepath.Join(c.cfg.CacheDir, cacheFileName)
	tempCachePath := cachePath + ".tmp"

	if err := os.WriteFile(tempCachePath, data, 0644); err != nil {
		return fmt.Errorf("write temp cache file: %w", err)
	}

	if err := os.Rename(tempCachePath, cachePath); err != nil {
		return fmt.Errorf("rename cache file: %w", err)
	}

	slog.Info("cache saved",
		"path", cachePath,
		"uri_count", len(c.cache.Uri),
		"html_count", len(c.cache.Html),
	)

	return nil
}

func (c *Converter) saveGeoJson(bookmark *Bookmark) error {
	fc := geojson.NewFeatureCollection()
	for _, item := range bookmark.Children {
		feature := geojson.NewFeature(orb.Point{item.Lon, item.Lat})
		feature.Properties["name"] = item.Name
		feature.Properties["marker-color"] = bookmark.Color
		feature.Properties["description"] = item.Description
		feature.Properties["Text"] = item.Text
		fc.Append(feature)
	}
	data, _ := fc.MarshalJSON()
	slog.Debug("feature collection", "json", string(data))

	outputFileName := fmt.Sprintf("%s-rev%d.geojson", bookmark.Title, bookmark.Revision)
	outputPath := filepath.Join(c.cfg.OutputDir, outputFileName)

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}

	return nil
}

func (c *Converter) convert(ctx context.Context) error {
	htmlBookmark, err := c.getHtmlBookmark(ctx, c.cfg.PublicId)
	if err != nil {
		return err
	}

	bookmark, err := parseHtmlBookmark(htmlBookmark)
	if err != nil {
		return err
	}

	for _, item := range bookmark.Children {
		u, err := url.Parse(item.Uri)
		if err != nil {
			return err
		}

		switch u.Host {
		case "org", "geo":
		case "pin":
			if len(u.Query()["ll"]) == 0 {
				slog.Warn("no ll param", "uri", item.Uri)
				continue
			}

			_, _ = fmt.Sscanf(u.Query()["ll"][0], "%f,%f", &item.Lon, &item.Lat)

			bookmark.Children[item.Id] = item

			continue
		default:
			slog.Warn("unsupported host", "host", u.Host)
		}

		htmlMetadata, err := c.getHtmlGeocodeMetadata(ctx, item.Uri)
		if err != nil {
			return err
		}

		item, err := parseHtmlGeocodeMetadata(htmlMetadata, item)
		if err != nil {
			return err
		}

		slog.Info("child", "bookmark", item)

		bookmark.Children[item.Id] = item
	}

	js, _ := json.MarshalIndent(bookmark, "", "  ")
	slog.Debug("bookmark", "json", string(js))

	if err := c.saveGeoJson(bookmark); err != nil {
		return err
	}

	return nil
}

func (c *Converter) getHtmlBookmark(ctx context.Context, publicId string) ([]byte, error) {
	if item, ok := c.cache.Html[publicId]; ok && time.Since(item.Updated) < cacheExpiration {
		slog.Info("cache hit", "public_id", publicId)

		return item.Data, nil
	}

	body, err := c.getHtmlBookmarkViaApi(ctx, publicId)
	if err != nil {
		return nil, err
	}

	c.cache.Html[publicId] = CacheItem{
		Updated: time.Now(),
		Data:    body,
	}

	return body, nil
}

func (c *Converter) getHtmlBookmarkViaApi(ctx context.Context, publicId string) ([]byte, error) {
	slog.Info("getHtmlBookmarkViaApi", "public_id", publicId)

	u, err := url.Parse("https://yandex.ru/maps")
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("bookmarks[publicId]", publicId)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-GB,en;q=0.9")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err = resp.Body.Close(); err != nil {
			slog.Error("resp.Body.Close", "err", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

//

func (c *Converter) getHtmlGeocodeMetadata(ctx context.Context, uri string) ([]byte, error) {
	if item, ok := c.cache.Uri[uri]; ok {
		slog.Info("cache hit", "uri", uri)

		return item.Data, nil
	}

	body, err := c.getHtmlGeocodeMetadataViaApi(ctx, uri)
	if err != nil {
		return nil, err
	}

	time.Sleep(pauseAfterRequest)

	c.cache.Uri[uri] = CacheItem{
		Updated: time.Now(),
		Data:    body,
	}

	return body, nil
}

func (c *Converter) getHtmlGeocodeMetadataViaApi(ctx context.Context, uri string) ([]byte, error) {
	slog.Info("getHtmlGeocodeMetadataViaApi", "uri", uri)

	u, err := url.Parse("https://geocode-maps.yandex.ru/v1")
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("apikey", c.cfg.ApiKey)
	q.Set("uri", uri)
	q.Set("format", "json")
	q.Set("language", "ru_RU")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err = resp.Body.Close(); err != nil {
			slog.Error("resp.Body.Close", "err", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func parseHtmlGeocodeMetadata(body []byte, bookmark BookmarkChildren) (BookmarkChildren, error) {
	slog.Debug("parseHtmlGeocodeMetadata", "body", string(body))

	if len(gjson.Get(string(body), "response.GeoObjectCollection.featureMember").Array()) == 0 {
		slog.Warn("empty featureMember",
			"title", bookmark.Title,
			"uri", bookmark.Uri,
			"body", string(body),
		)

		return bookmark, nil
	}

	obj := gjson.Get(string(body), "response.GeoObjectCollection.featureMember.0.GeoObject")

	pos := obj.Get("Point.pos").String()
	_, err := fmt.Sscanf(pos, "%f %f", &bookmark.Lon, &bookmark.Lat)
	if err != nil {
		return bookmark, fmt.Errorf("failed to parse pos: %w", err)
	}

	bookmark.Name = obj.Get("name").String()
	bookmark.Text = obj.Get("metaDataProperty.GeocoderMetaData.text").String()

	js, _ := json.MarshalIndent(json.RawMessage(obj.String()), "", "  ")
	slog.Debug("parseHtmlGeocodeMetadata", "obj", string(js))

	return bookmark, nil
}

//

type Bookmark struct {
	Revision    int64
	PublicId    string
	Title       string
	Description string
	Author      string
	Color       string
	Children    map[string]BookmarkChildren
}

type BookmarkChildren struct {
	Id          string
	Uri         string
	Title       string
	Description string

	// Properties
	Name  string
	Text  string
	Lat   float64
	Lon   float64
	Extra map[string]string
}

func parseHtmlBookmark(htmlBookmark []byte) (*Bookmark, error) {
	slog.Debug("parseHtmlBookmark", "htmlBookmark", string(htmlBookmark))

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlBookmark))
	if err != nil {
		return nil, err
	}

	data := doc.Find(`script[type="application/json"].state-view`).Text()

	cfg := gjson.Get(data, "config.bookmarksPublicList")

	js, _ := json.MarshalIndent(json.RawMessage(cfg.String()), "", "  ")
	slog.Debug("parseHtmlBookmark", "json", string(js))

	if cfg.String() == "" {
		return nil, fmt.Errorf("no config found")
	}

	children := make(map[string]BookmarkChildren)

	cfg.Get("children").ForEach(func(key, value gjson.Result) bool {
		id := value.Get("id").String()
		uri := value.Get("uri").String()
		title := value.Get("title").String()
		description := value.Get("description").String()

		children[id] = BookmarkChildren{
			Id:          id,
			Uri:         uri,
			Title:       title,
			Description: description,
		}

		return true
	})

	_, color, _ := strings.Cut(cfg.Get("icon").String(), ":")

	bookmark := Bookmark{
		Revision:    cfg.Get("revision").Int(),
		PublicId:    cfg.Get("publicId").String(),
		Title:       cfg.Get("title").String(),
		Description: cfg.Get("description").String(),
		Author:      cfg.Get("author").String(),
		Color:       color,
		Children:    children,
	}

	return &bookmark, nil
}
