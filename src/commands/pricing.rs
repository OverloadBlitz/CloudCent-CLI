use std::collections::HashMap;
use std::sync::Arc;
use anyhow::Result;

use crate::api::{CloudCentClient, Config, PricingApiResponse};
use crate::db::Database;

/// Pricing options loaded from API
#[derive(Debug, Clone, Default)]
#[allow(dead_code)]
pub struct PricingOptions {
    pub products: Vec<String>,
    pub regions: Vec<String>,
    /// Map from product to its regions
    pub product_regions: HashMap<String, Vec<String>>,
    /// Map from product to its attribute key names
    pub product_attrs: HashMap<String, Vec<String>>,
    /// Map from product to its attributes and their values
    pub attribute_values: HashMap<String, HashMap<String, Vec<String>>>,
    /// Map from product to its group number
    pub product_groups: HashMap<String, u64>,
}

pub struct PricingCommand {
    client: CloudCentClient,
    db: Option<Database>,
}


impl PricingCommand {
    #[allow(dead_code)]
    pub fn new() -> Self {
        let mut client = CloudCentClient::new();
        let _ = client.load_config();
        let db = Database::new().ok();
        Self { client, db }
    }

    pub fn with_client(client: CloudCentClient) -> Self {
        let db = Database::new().ok();
        Self { client, db }
    }

    /// Load pricing options from metadata API (with caching)
    pub async fn load_options(&self) -> Result<Arc<PricingOptions>, String> {
        // Try to load from cache first
        if let Some(ref db) = self.db {
            if let Ok(Some(metadata)) = db.get_metadata_cache() {
                return Ok(Arc::new(Self::process_metadata(metadata)?));
            }
        }
        
        // Fetch from API if not in cache
        let metadata = self
            .client
            .get_metadata()
            .await
            .map_err(|e| format!("get_metadata failed: {}", e))?;
        
        // Save to cache
        if let Some(ref db) = self.db {
            let _ = db.set_metadata_cache(&metadata);
        }
        
        Ok(Arc::new(Self::process_metadata(metadata)?))
    }
    
    /// Force refresh metadata from API (bypass cache)
    pub async fn refresh_metadata(&self) -> Result<Arc<PricingOptions>, String> {
        // Clear cache
        if let Some(ref db) = self.db {
            let _ = db.clear_metadata_cache();
        }
        
        // Fetch fresh data
        let metadata = self
            .client
            .get_metadata()
            .await
            .map_err(|e| format!("get_metadata failed: {}", e))?;
        
        // Save to cache
        if let Some(ref db) = self.db {
            let _ = db.set_metadata_cache(&metadata);
        }
        
        Ok(Arc::new(Self::process_metadata(metadata)?))
    }
    
    fn process_metadata(metadata: crate::api::MetadataResponse) -> Result<PricingOptions, String> {
        // Products are the keys of product_regions
        let mut products: Vec<String> = metadata.product_regions.keys().cloned().collect();
        products.sort();
        
        // Extract all unique regions from all products
        let mut regions_set = std::collections::HashSet::new();
        for regions in metadata.product_regions.values() {
            for region in regions {
                if !region.is_empty() {
                    regions_set.insert(region.clone());
                }
            }
        }
        let mut regions: Vec<String> = regions_set.into_iter().collect();
        regions.sort();
        
        Ok(PricingOptions {
            products,
            regions,
            product_regions: metadata.product_regions,
            product_attrs: metadata.product_attrs,
            attribute_values: metadata.attribute_values,
            product_groups: metadata.product_groups,
        })
    }

    /// Load pricing options from local metadata file
    pub async fn load_metadata_from_file(&self) -> Result<Arc<PricingOptions>, String> {
        let config_dir = dirs::home_dir()
            .ok_or_else(|| "Failed to get home directory".to_string())?
            .join(".cloudcent");
        let file_path = config_dir.join("metadata.json.gz");
        
        if !file_path.exists() {
            return Err("Metadata file not found. Please sync first.".to_string());
        }
        
        let content = std::fs::read(&file_path).map_err(|e| format!("Failed to read metadata file: {}", e))?;
        
        // Decompress GZip
        use flate2::read::GzDecoder;
        use std::io::Read;
        let mut decoder = GzDecoder::new(&content[..]);
        let mut json_content = String::new();
        decoder.read_to_string(&mut json_content).map_err(|e| format!("Failed to decompress metadata: {}", e))?;
        
        let metadata: crate::api::MetadataResponse = serde_json::from_str(&json_content).map_err(|e| format!("Failed to parse metadata JSON: {}", e))?;
        
        Ok(Arc::new(Self::process_metadata(metadata)?))
    }

    /// Fetch pricing data
    #[allow(dead_code)]
    pub async fn fetch_pricing(
        &self,
        products: &[String],
        regions: &[String],
        attrs: std::collections::HashMap<String, String>,
        prices: &[String]
    ) -> Result<PricingApiResponse, String> {
        self.client
            .fetch_pricing_multi(products, regions, attrs, prices)
            .await
            .map_err(|e| e.to_string())
    }
}

/// Helper to load options in a background thread (for TUI)
#[allow(dead_code)]
pub fn load_options_async(config: Option<Config>) -> Result<Arc<PricingOptions>, String> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| e.to_string())?;
    rt.block_on(async {
        let mut client = CloudCentClient::new();
        if let Some(cfg) = config {
            client.set_config(cfg);
        }
        let cmd = PricingCommand::with_client(client);
        cmd.load_options().await
    })
}

/// Helper to load metadata from local file in a background thread
#[allow(dead_code)]
pub fn load_metadata_async(config: Option<Config>) -> Result<Arc<PricingOptions>, String> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| e.to_string())?;
    rt.block_on(async {
        let mut client = CloudCentClient::new();
        if let Some(cfg) = config {
            client.set_config(cfg);
        }
        let cmd = PricingCommand::with_client(client);
        cmd.load_metadata_from_file().await
    })
}

/// Helper to refresh metadata in a background thread (for TUI)
#[allow(dead_code)]
pub fn refresh_metadata_async(config: Option<Config>) -> Result<Arc<PricingOptions>, String> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| e.to_string())?;
    rt.block_on(async {
        let mut client = CloudCentClient::new();
        if let Some(cfg) = config {
            client.set_config(cfg);
        }
        let cmd = PricingCommand::with_client(client);
        cmd.refresh_metadata().await
    })
}

/// Helper to fetch pricing in a background thread (for TUI)
#[allow(dead_code)]
pub fn fetch_pricing_async(
    config: Option<Config>,
    products: Vec<String>,
    regions: Vec<String>,
    attrs: std::collections::HashMap<String, String>,
    prices: Vec<String>,
) -> Result<PricingApiResponse, String> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| e.to_string())?;
    rt.block_on(async {
        let mut client = CloudCentClient::new();
        if let Some(cfg) = config {
            client.set_config(cfg);
        }
        let cmd = PricingCommand::with_client(client);
        cmd.fetch_pricing(&products, &regions, attrs, &prices)
            .await
    })
}
