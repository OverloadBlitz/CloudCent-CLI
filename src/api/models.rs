use std::collections::HashMap;
use std::fmt;
use serde::{Deserialize, Serialize};
use indexmap::IndexMap;

/// Attribute value that can be any JSON type (string, number, array, etc.)
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(transparent)]
pub struct AttrValue(pub serde_json::Value);

impl fmt::Display for AttrValue {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match &self.0 {
            serde_json::Value::String(s) => write!(f, "{}", s),
            serde_json::Value::Number(n) => write!(f, "{}", n),
            serde_json::Value::Array(arr) => {
                let parts: Vec<String> = arr.iter().map(|v| match v {
                    serde_json::Value::String(s) => s.clone(),
                    other => other.to_string(),
                }).collect();
                write!(f, "{}", parts.join(", "))
            }
            serde_json::Value::Bool(b) => write!(f, "{}", b),
            serde_json::Value::Null => write!(f, ""),
            other => write!(f, "{}", other),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Config {
    pub cli_id: String,
    pub api_key: Option<String>,
}

/// Rate within a price
#[allow(dead_code)]
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PriceRate {
    #[serde(default)]
    pub price: Option<serde_json::Value>,
    #[serde(rename = "startRange", default)]
    pub start_range: Option<serde_json::Value>,
    #[serde(rename = "endRange", default)]
    pub end_range: Option<serde_json::Value>,
}

#[allow(dead_code)]
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Price {
    #[serde(default)]
    pub rates: Option<Vec<PriceRate>>,
    #[serde(rename = "pricingModel", default)]
    pub pricing_model: Option<String>,
    #[serde(rename = "upfrontFee", default)]
    pub upfront_fee: Option<serde_json::Value>,
    #[serde(default)]
    pub year: Option<serde_json::Value>,
    #[serde(default)]
    pub unit: Option<String>,
    #[serde(rename = "currencyCode", default)]
    pub currency_code: Option<String>,
    #[serde(rename = "purchaseOption", default)]
    pub purchase_option: Option<String>,
}

#[allow(dead_code)]
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PricingItem {
    pub product: String,
    #[serde(default)]
    pub provider: String,
    pub region: String,
    #[serde(default)]
    pub attributes: IndexMap<String, Option<AttrValue>>,
    pub prices: Vec<Price>,
    #[serde(rename = "minPrice", default)]
    pub min_price: Option<serde_json::Value>,
    #[serde(rename = "maxPrice", default)]
    pub max_price: Option<serde_json::Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PricingApiResponse {
    pub data: Vec<PricingItem>,
    #[serde(default)]
    pub total: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PricingRequest {
    pub attrs: HashMap<String, String>,
    pub prices: Vec<String>,
}

/// Metadata API response structure
#[allow(dead_code)]
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MetadataResponse {
    pub product_regions: HashMap<String, Vec<String>>,
    #[serde(default)]
    pub product_attrs: HashMap<String, Vec<String>>,
    #[serde(default)]
    pub attribute_values: HashMap<String, HashMap<String, Vec<String>>>,
    #[serde(default)]
    pub product_groups: HashMap<String, u64>,
}

/// Generate token API response
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GenerateTokenResponse {
    #[serde(alias = "token")]
    pub access_token: String,
    #[serde(alias = "exchangeId", alias = "exchange_id")]
    pub exchange_code: String,
}

/// Exchange API response
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExchangeResponse {
    #[serde(default)]
    pub status: Option<String>,
    #[serde(default, alias = "cliId")]
    pub cli_id: Option<String>,
    #[serde(default, alias = "apiKey")]
    pub api_key: Option<String>,
}
