use anyhow::{Context, Result};
use chrono::{DateTime, Duration, Utc};
use rusqlite::{params, Connection};
use std::path::PathBuf;

use crate::api::{PricingApiResponse, MetadataResponse};

const CACHE_TTL_DAYS: i64 = 3;
#[allow(dead_code)]
const METADATA_CACHE_TTL_HOURS: i64 = 24;

pub struct Database {
    conn: Connection,
}

#[derive(Debug, Clone)]
pub struct QueryHistory {
    pub id: i64,
    #[allow(dead_code)]
    pub providers: String,
    pub regions: String,
    #[allow(dead_code)]
    pub categories: String,
    pub product_families: String,
    pub attributes: String,
    pub prices: String,
    pub result_count: u64,
    pub cache_key: String,
    pub created_at: DateTime<Utc>,
}

impl Database {
    pub fn new() -> Result<Self> {
        let db_path = Self::get_db_path()?;
        if let Some(parent) = db_path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        
        let conn = Connection::open(&db_path)
            .context("Failed to open database")?;
        
        let db = Self { conn };
        db.init_tables()?;
        Ok(db)
    }
    
    fn get_db_path() -> Result<PathBuf> {
        let config_dir = dirs::home_dir()
            .context("Failed to get home directory")?
            .join(".cloudcent");
        Ok(config_dir.join("cloudcent.db"))
    }
    
    fn init_tables(&self) -> Result<()> {
        self.conn.execute(
            "CREATE TABLE IF NOT EXISTS query_history (
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
            )",
            [],
        )?;
        
        // Migrations: add columns if they don't exist (for older DBs)
        let _ = self.conn.execute("ALTER TABLE query_history ADD COLUMN cache_key TEXT NOT NULL DEFAULT ''", []);
        let _ = self.conn.execute("ALTER TABLE query_history ADD COLUMN attributes TEXT NOT NULL DEFAULT ''", []);
        let _ = self.conn.execute("ALTER TABLE query_history ADD COLUMN prices TEXT NOT NULL DEFAULT ''", []);
        
        self.conn.execute(
            "CREATE TABLE IF NOT EXISTS pricing_cache (
                cache_key TEXT PRIMARY KEY,
                response_json TEXT NOT NULL,
                created_at TEXT NOT NULL
            )",
            [],
        )?;
        
        self.conn.execute(
            "CREATE INDEX IF NOT EXISTS idx_cache_created ON pricing_cache(created_at)",
            [],
        )?;
        
        self.conn.execute(
            "CREATE TABLE IF NOT EXISTS metadata_cache (
                id INTEGER PRIMARY KEY CHECK (id = 1),
                metadata_json TEXT NOT NULL,
                created_at TEXT NOT NULL
            )",
            [],
        )?;
        
        self.conn.execute(
            "CREATE TABLE IF NOT EXISTS estimates (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL UNIQUE,
                estimate_json TEXT NOT NULL,
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )",
            [],
        )?;
        
        Ok(())
    }

    // === History Methods ===
    
    pub fn add_history(
        &self,
        providers: &[String],
        regions: &[String],
        categories: &[String],
        product_families: &[String],
        attributes: &[String],
        prices: &[String],
        result_count: u64,
        cache_key: &str,
    ) -> Result<i64> {
        let now = Utc::now().to_rfc3339();
        
        self.conn.execute(
            "INSERT INTO query_history (providers, regions, categories, product_families, attributes, prices, result_count, cache_key, created_at)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)",
            params![
                providers.join(","),
                regions.join(","),
                categories.join(","),
                product_families.join(","),
                attributes.join(","),
                prices.join(","),
                result_count as i64,
                cache_key,
                now
            ],
        )?;
        
        Ok(self.conn.last_insert_rowid())
    }
    
    pub fn get_history(&self, limit: usize) -> Result<Vec<QueryHistory>> {
        let mut stmt = self.conn.prepare(
            "SELECT id, providers, regions, categories, product_families, attributes, prices, result_count, cache_key, created_at
             FROM query_history
             ORDER BY created_at DESC
             LIMIT ?1"
        )?;

        let rows = stmt.query_map([limit as i64], |row| {
            let created_str: String = row.get(9)?;
            let created_at = DateTime::parse_from_rfc3339(&created_str)
                .map(|dt| dt.with_timezone(&Utc))
                .unwrap_or_else(|_| Utc::now());
            
            Ok(QueryHistory {
                id: row.get(0)?,
                providers: row.get(1)?,
                regions: row.get(2)?,
                categories: row.get(3)?,
                product_families: row.get(4)?,
                attributes: row.get(5)?,
                prices: row.get(6)?,
                result_count: row.get::<_, i64>(7)? as u64,
                cache_key: row.get(8)?,
                created_at,
            })
        })?;
        
        let mut history = Vec::new();
        for row in rows {
            history.push(row?);
        }
        Ok(history)
    }
    
    #[allow(dead_code)]
    pub fn delete_history(&self, id: i64) -> Result<()> {
        self.conn.execute("DELETE FROM query_history WHERE id = ?1", [id])?;
        Ok(())
    }

    #[allow(dead_code)]
    pub fn clear_history(&self) -> Result<()> {
        self.conn.execute("DELETE FROM query_history", [])?;
        Ok(())
    }
    
    pub fn clear_all(&self) -> Result<()> {
        self.conn.execute("DELETE FROM query_history", [])?;
        self.conn.execute("DELETE FROM pricing_cache", [])?;
        Ok(())
    }
    
    // === Cache Methods ===
    
    pub fn get_cache(&self, cache_key: &str) -> Result<Option<PricingApiResponse>> {
        let cutoff = (Utc::now() - Duration::days(CACHE_TTL_DAYS)).to_rfc3339();
        
        let mut stmt = self.conn.prepare(
            "SELECT response_json FROM pricing_cache 
             WHERE cache_key = ?1 AND created_at > ?2"
        )?;
        
        let result: Option<String> = stmt
            .query_row(params![cache_key, cutoff], |row| row.get(0))
            .ok();
        
        if let Some(json) = result {
            let response: PricingApiResponse = serde_json::from_str(&json)?;
            return Ok(Some(response));
        }
        
        Ok(None)
    }
    
    pub fn set_cache(&self, cache_key: &str, response: &PricingApiResponse) -> Result<()> {
        let json = serde_json::to_string(response)?;
        let now = Utc::now().to_rfc3339();
        
        self.conn.execute(
            "INSERT OR REPLACE INTO pricing_cache (cache_key, response_json, created_at)
             VALUES (?1, ?2, ?3)",
            params![cache_key, json, now],
        )?;
        
        Ok(())
    }
    
    #[allow(dead_code)]
    pub fn cleanup_expired_cache(&self) -> Result<usize> {
        let cutoff = (Utc::now() - Duration::days(CACHE_TTL_DAYS)).to_rfc3339();
        let deleted = self.conn.execute(
            "DELETE FROM pricing_cache WHERE created_at <= ?1",
            [cutoff],
        )?;
        Ok(deleted)
    }
    
    pub fn get_cache_stats(&self) -> Result<(usize, usize)> {
        // Returns (count, size_in_bytes)
        let count: i64 = self.conn.query_row(
            "SELECT COUNT(*) FROM pricing_cache",
            [],
            |row| row.get(0),
        )?;
        
        let size: i64 = self.conn.query_row(
            "SELECT COALESCE(SUM(LENGTH(response_json)), 0) FROM pricing_cache",
            [],
            |row| row.get(0),
        )?;
        
        Ok((count as usize, size as usize))
    }
    
    #[allow(dead_code)]
    pub fn clear_cache(&self) -> Result<usize> {
        let deleted = self.conn.execute("DELETE FROM pricing_cache", [])?;
        Ok(deleted)
    }
    
    pub fn make_cache_key(
        providers: &[String],
        regions: &[String],
        categories: &[String],
        product_families: &[String],
        attributes: &std::collections::HashMap<String, String>,
        prices: &[String],
    ) -> String {
        let mut parts: Vec<String> = vec![
            providers.join(","),
            regions.join(","),
            categories.join(","),
            product_families.join(","),
        ];
        
        // Add attributes to key
        let mut attr_parts: Vec<String> = attributes.iter()
            .map(|(k, v)| format!("{}={}", k, v))
            .collect();
        attr_parts.sort();
        parts.push(attr_parts.join("&"));
        
        parts.push(prices.join(","));
        
        parts.sort();
        parts.join("|")
    }
    
    // === Metadata Cache Methods ===

    #[allow(dead_code)]
    pub fn get_metadata_cache(&self) -> Result<Option<MetadataResponse>> {
        let cutoff = (Utc::now() - Duration::hours(METADATA_CACHE_TTL_HOURS)).to_rfc3339();
        
        let mut stmt = self.conn.prepare(
            "SELECT metadata_json FROM metadata_cache 
             WHERE id = 1 AND created_at > ?1"
        )?;
        
        let result: Option<String> = stmt
            .query_row([cutoff], |row| row.get(0))
            .ok();
        
        if let Some(json) = result {
            let metadata: MetadataResponse = serde_json::from_str(&json)?;
            return Ok(Some(metadata));
        }
        
        Ok(None)
    }
    
    #[allow(dead_code)]
    pub fn set_metadata_cache(&self, metadata: &MetadataResponse) -> Result<()> {
        let json = serde_json::to_string(metadata)?;
        let now = Utc::now().to_rfc3339();
        
        self.conn.execute(
            "INSERT OR REPLACE INTO metadata_cache (id, metadata_json, created_at)
             VALUES (1, ?1, ?2)",
            params![json, now],
        )?;
        
        Ok(())
    }
    
    #[allow(dead_code)]
    pub fn clear_metadata_cache(&self) -> Result<()> {
        self.conn.execute("DELETE FROM metadata_cache WHERE id = 1", [])?;
        Ok(())
    }

    // === Estimate Persistence Methods ===

    #[allow(dead_code)]
    pub fn save_estimate(&self, name: &str, estimate_json: &str) -> Result<i64> {
        let now = Utc::now().to_rfc3339();
        // Upsert by name
        self.conn.execute(
            "INSERT OR REPLACE INTO estimates (name, estimate_json, created_at, updated_at)
             VALUES (?1, ?2, COALESCE((SELECT created_at FROM estimates WHERE name = ?1), ?3), ?3)",
            params![name, estimate_json, now],
        )?;
        Ok(self.conn.last_insert_rowid())
    }

    #[allow(dead_code)]
    pub fn load_estimates(&self) -> Result<Vec<(i64, String, String)>> {
        let mut stmt = self.conn.prepare(
            "SELECT id, name, estimate_json FROM estimates ORDER BY updated_at DESC"
        )?;
        let rows = stmt.query_map([], |row| {
            Ok((row.get::<_, i64>(0)?, row.get::<_, String>(1)?, row.get::<_, String>(2)?))
        })?;
        let mut results = Vec::new();
        for row in rows { results.push(row?); }
        Ok(results)
    }

    #[allow(dead_code)]
    pub fn delete_estimate(&self, id: i64) -> Result<()> {
        self.conn.execute("DELETE FROM estimates WHERE id = ?1", [id])?;
        Ok(())
    }
}
