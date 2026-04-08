use std::collections::HashMap;
use std::sync::Arc;
use anyhow::Result;

/// Pricing options loaded from the metadata endpoint.
#[derive(Debug, Clone, Default)]
pub struct PricingOptions {
    pub products: Vec<String>,
    pub regions: Vec<String>,
    /// Map from product key to its available regions.
    pub product_regions: HashMap<String, Vec<String>>,
    /// Map from product key to its attribute key names.
    pub product_attrs: HashMap<String, Vec<String>>,
    /// Map from product key to its attributes and their possible values.
    pub attribute_values: HashMap<String, HashMap<String, Vec<String>>>,
    /// Map from product key to its comparison group number.
    pub product_groups: HashMap<String, u64>,
}

fn process_metadata(metadata: crate::api::MetadataResponse) -> Result<PricingOptions, String> {
    let mut products: Vec<String> = metadata.product_regions.keys().cloned().collect();
    products.sort();

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

/// Read and decompress the local metadata.json.gz file.
async fn load_metadata_from_file() -> Result<Arc<PricingOptions>, String> {
    let file_path = dirs::home_dir()
        .ok_or_else(|| "Failed to get home directory".to_string())?
        .join(".cloudcent")
        .join("metadata.json.gz");

    if !file_path.exists() {
        return Err("Metadata file not found. Please sync first.".to_string());
    }

    let content = std::fs::read(&file_path)
        .map_err(|e| format!("Failed to read metadata file: {}", e))?;

    use flate2::read::GzDecoder;
    use std::io::Read;
    let mut decoder = GzDecoder::new(&content[..]);
    let mut json_content = String::new();
    decoder
        .read_to_string(&mut json_content)
        .map_err(|e| format!("Failed to decompress metadata: {}", e))?;

    let metadata: crate::api::MetadataResponse = serde_json::from_str(&json_content)
        .map_err(|e| format!("Failed to parse metadata JSON: {}", e))?;

    Ok(Arc::new(process_metadata(metadata)?))
}

/// Load metadata from the local file on a blocking thread.
/// Only reads `~/.cloudcent/metadata.json.gz` — no network access required.
pub fn load_metadata_async() -> Result<Arc<PricingOptions>, String> {
    let rt = tokio::runtime::Runtime::new().map_err(|e| e.to_string())?;
    rt.block_on(load_metadata_from_file())
}
