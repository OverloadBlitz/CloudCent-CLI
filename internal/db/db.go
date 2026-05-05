package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"github.com/OverloadBlitz/cloudcent-cli/internal/config"
	_ "modernc.org/sqlite"
)

const cacheTTLDays = 3

// DB wraps an SQLite connection.
type DB struct {
	conn *sql.DB
}

// QueryHistory is a single history entry.
type QueryHistory struct {
	ID             int64
	Providers      string
	Regions        string
	Categories     string
	ProductFamilies string
	Attributes     string
	Prices         string
	ResultCount    int64
	CacheKey       string
	CreatedAt      time.Time
}

// New opens (or creates) the SQLite database and runs migrations.
func New() (*DB, error) {
	p, err := config.DBPath()
	if err != nil {
		return nil, err
	}
	conn, err := sql.Open("sqlite", p)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	d := &DB{conn: conn}
	if err := d.initTables(); err != nil {
		conn.Close()
		return nil, err
	}
	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) initTables() error {
	_, err := d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS query_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			providers TEXT NOT NULL,
			regions TEXT NOT NULL,
			categories TEXT NOT NULL,
			product_families TEXT NOT NULL,
			attributes TEXT NOT NULL DEFAULT '',
			prices TEXT NOT NULL DEFAULT '',
			result_count INTEGER NOT NULL,
			cache_key TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`)
	if err != nil {
		return err
	}
	// Migrations for older DBs (ignore errors — columns may already exist)
	d.conn.Exec("ALTER TABLE query_history ADD COLUMN cache_key TEXT NOT NULL DEFAULT ''")
	d.conn.Exec("ALTER TABLE query_history ADD COLUMN attributes TEXT NOT NULL DEFAULT ''")
	d.conn.Exec("ALTER TABLE query_history ADD COLUMN prices TEXT NOT NULL DEFAULT ''")

	_, err = d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS pricing_cache (
			cache_key TEXT PRIMARY KEY,
			response_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`)
	if err != nil {
		return err
	}
	d.conn.Exec("CREATE INDEX IF NOT EXISTS idx_cache_created ON pricing_cache(created_at)")

	_, err = d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS metadata_cache (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			metadata_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`)
	if err != nil {
		return err
	}

	_, err = d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS estimates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			estimate_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`)
	return err
}

// AddHistory records a pricing query in history.
func (d *DB) AddHistory(providers, regions, categories, productFamilies, attributes, prices []string, resultCount int64, cacheKey string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := d.conn.Exec(
		`INSERT INTO query_history (providers, regions, categories, product_families, attributes, prices, result_count, cache_key, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.Join(providers, ","),
		strings.Join(regions, ","),
		strings.Join(categories, ","),
		strings.Join(productFamilies, ","),
		strings.Join(attributes, ","),
		strings.Join(prices, ","),
		resultCount,
		cacheKey,
		now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetHistory returns the most recent n history entries.
func (d *DB) GetHistory(limit int) ([]QueryHistory, error) {
	rows, err := d.conn.Query(
		`SELECT id, providers, regions, categories, product_families, attributes, prices, result_count, cache_key, created_at
		 FROM query_history ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []QueryHistory
	for rows.Next() {
		var h QueryHistory
		var createdStr string
		err := rows.Scan(&h.ID, &h.Providers, &h.Regions, &h.Categories, &h.ProductFamilies,
			&h.Attributes, &h.Prices, &h.ResultCount, &h.CacheKey, &createdStr)
		if err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, createdStr)
		if err != nil {
			t = time.Now()
		}
		h.CreatedAt = t
		result = append(result, h)
	}
	return result, rows.Err()
}

// ClearAll deletes all history and pricing cache.
func (d *DB) ClearAll() error {
	if _, err := d.conn.Exec("DELETE FROM query_history"); err != nil {
		return err
	}
	_, err := d.conn.Exec("DELETE FROM pricing_cache")
	return err
}

// GetCache returns a cached pricing response for the given key if not expired.
func (d *DB) GetCache(cacheKey string) (*api.PricingAPIResponse, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(cacheTTLDays) * 24 * time.Hour).Format(time.RFC3339)
	var jsonStr string
	err := d.conn.QueryRow(
		"SELECT response_json FROM pricing_cache WHERE cache_key = ? AND created_at > ?",
		cacheKey, cutoff,
	).Scan(&jsonStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var resp api.PricingAPIResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetCache stores or replaces a pricing response in the cache.
func (d *DB) SetCache(cacheKey string, resp *api.PricingAPIResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.conn.Exec(
		"INSERT OR REPLACE INTO pricing_cache (cache_key, response_json, created_at) VALUES (?, ?, ?)",
		cacheKey, string(data), now,
	)
	return err
}

// GetCacheStats returns (count, totalBytes) for pricing_cache.
func (d *DB) GetCacheStats() (int, int, error) {
	var count, size int
	err := d.conn.QueryRow("SELECT COUNT(*), COALESCE(SUM(LENGTH(response_json)),0) FROM pricing_cache").Scan(&count, &size)
	return count, size, err
}

// MakeCacheKey produces a deterministic cache key from query parameters.
func MakeCacheKey(providers, regions, categories, productFamilies []string, attrs map[string]string, prices []string) string {
	parts := []string{
		strings.Join(providers, ","),
		strings.Join(regions, ","),
		strings.Join(categories, ","),
		strings.Join(productFamilies, ","),
	}
	attrParts := make([]string, 0, len(attrs))
	for k, v := range attrs {
		attrParts = append(attrParts, k+"="+v)
	}
	sort.Strings(attrParts)
	parts = append(parts, strings.Join(attrParts, "&"))
	parts = append(parts, strings.Join(prices, ","))
	sort.Strings(parts)
	return strings.Join(parts, "|")
}
