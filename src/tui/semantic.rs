use std::collections::HashMap;
use std::sync::LazyLock;

use fuzzy_matcher::FuzzyMatcher;
use fuzzy_matcher::skim::SkimMatcherV2;

/// Generic user terms mapped to keywords found in product IDs / display names.
pub static PRODUCT_ALIASES: LazyLock<HashMap<&'static str, Vec<&'static str>>> =
    LazyLock::new(|| {
        let mut m: HashMap<&'static str, Vec<&'static str>> = HashMap::new();
        m.insert("compute",      vec!["ec2", "compute engine", "virtual machine", "vm", "ecs", "lambda", "cloud functions", "app engine", "instances", "batch", "mac_compute", "bare_metal"]);
        m.insert("vm",           vec!["ec2", "compute engine", "virtual machine", "instances", "vps"]);
        m.insert("storage",      vec!["s3", "cloud storage", "blob storage", "ebs", "efs", "glacier", "object storage", "archive_storage", "backup_storage", "file_storage"]);
        m.insert("database",     vec!["rds", "dynamodb", "cloud sql", "cosmos", "aurora", "redshift", "firestore", "bigtable", "spanner", "mysql", "postgres", "alloydb", "distributed_sql", "nosql_instance", "rds_instance", "timeseries_db"]);
        m.insert("db",           vec!["rds", "dynamodb", "cloud sql", "cosmos", "aurora", "redshift", "firestore", "alloydb", "nosql"]);
        m.insert("cdn",          vec!["cloudfront", "cloud cdn", "azure cdn", "media", "edge"]);
        m.insert("serverless",   vec!["lambda", "cloud functions", "azure functions", "fargate", "cloud run", "app service", "app_hosting"]);
        m.insert("container",    vec!["ecs", "eks", "gke", "aks", "fargate", "cloud run", "kubernetes"]);
        m.insert("k8s",          vec!["eks", "gke", "aks", "kubernetes"]);
        m.insert("cache",        vec!["elasticache", "memorystore", "redis", "memcached", "cache_instance"]);
        m.insert("queue",        vec!["sqs", "pub/sub", "service bus", "eventbridge", "sns", "mq", "messaging"]);
        m.insert("network",      vec!["vpc", "nat", "load balancer", "elb", "alb", "nlb", "route 53", "dns", "firewall", "ip_address", "nat_gateway"]);
        m.insert("dns",          vec!["route 53", "cloud dns", "route53"]);
        m.insert("lb",           vec!["elb", "alb", "nlb", "load balancer", "cloud load balancing"]);
        m.insert("monitor",      vec!["cloudwatch", "cloud monitoring", "azure monitor", "stackdriver", "metrics"]);
        m.insert("logging",      vec!["cloudwatch", "cloud logging", "log analytics", "stackdriver"]);
        m.insert("ml",           vec!["sagemaker", "vertex ai", "azure ml", "machine learning", "ai", "ccai"]);
        m.insert("ai",           vec!["sagemaker", "vertex ai", "azure ml", "cognitive", "rekognition", "machine learning", "ccai"]);
        m.insert("gpu",          vec!["ec2", "compute engine", "virtual machine", "accelerator", "gpu", "compute_gpu"]);
        m.insert("disk",         vec!["ebs", "persistent disk", "managed disk", "volume", "block", "compute_storage"]);
        m.insert("email",        vec!["ses", "email", "communication services"]);
        m.insert("secret",       vec!["secrets manager", "secret manager", "key vault", "kms"]);
        m.insert("auth",         vec!["cognito", "identity platform", "azure ad", "iam", "identity"]);
        m.insert("function",     vec!["lambda", "cloud functions", "azure functions", "cloud run"]);
        m
    });

/// Inverted map from product_id (or its common provider-specific name) to generic categories.
pub static PRODUCT_TO_CATEGORIES: LazyLock<HashMap<String, Vec<&'static str>>> =
    LazyLock::new(|| {
        let mut m: HashMap<String, Vec<&'static str>> = HashMap::new();
        for (category, aliases) in PRODUCT_ALIASES.iter() {
            // Self-mapping
            m.entry(category.to_string()).or_default().push(category);
            // Map aliases back to this category
            for alias in aliases {
                m.entry(alias.to_string()).or_default().push(category);
            }
        }
        m
    });

/// Attribute key substrings that imply a product category.
static ATTR_CATEGORY_HINTS: LazyLock<Vec<(&'static str, &'static str)>> = LazyLock::new(|| {
    vec![
        ("instancetype", "compute"),
        ("instance_type", "compute"),
        ("machinetype",  "compute"),
        ("machine_type",  "compute"),
        ("vcpu",         "compute"),
        ("memory",       "compute"),
        ("storagetype",  "storage"),
        ("storage_type",  "storage"),
        ("storageclass", "storage"),
        ("storage_class", "storage"),
        ("volumetype",   "storage"),
        ("volume_type",   "storage"),
        ("databaseengine", "database"),
        ("database_engine", "database"),
        ("engineversion",  "database"),
        ("cacheengine",  "cache"),
        ("nodetype",     "cache"),
        ("protocol",     "network"),
    ]
});

/// A single entry in the suggestion list.
#[derive(Clone, Debug)]
pub struct SuggestionItem {
    /// The value that will be inserted (product ID, provider name, …).
    pub value: String,
    /// Human-readable label shown in the list.
    pub display: String,
    /// Short hint explaining why this item matched (shown in gray).
    pub reason: String,
    /// True when the match came from the alias table or attribute inference.
    pub is_semantic: bool,
    /// True when the value is already in the selected tags for this field.
    pub already_selected: bool,
}

// ── Helpers ──────────────────────────────────────────────────────────────────

/// Infer a generic category for `product_id` by inspecting its attribute keys.
pub fn infer_category_from_attrs(
    product_id: &str,
    attribute_values: &HashMap<String, HashMap<String, Vec<String>>>,
) -> Option<&'static str> {
    let attrs = attribute_values.get(product_id)?;
    for key in attrs.keys() {
        let lower = key.to_lowercase();
        for (hint, category) in ATTR_CATEGORY_HINTS.iter() {
            if lower.contains(hint) {
                return Some(category);
            }
        }
    }
    None
}

// ── Suggestion generators ────────────────────────────────────────────────────

/// Score and rank products using a multi-dimensional matching algorithm:
///   1. Exact ID match (1000)
///   2. ID prefix match (500)
///   3. ID contains query (200)
///   4. Fuzzy match on ID (≤150)
///   5. Alias / synonym match (400, marks `is_semantic`)
///   6. Category inference from attribute keys (200, marks `is_semantic`)
///
/// When `selected_tags` is non-empty, only products in the same group are shown.
pub fn score_and_suggest_products(
    query: &str,
    products: &[String],
    attribute_values: &HashMap<String, HashMap<String, Vec<String>>>,
    product_groups: &HashMap<String, u64>,
    selected_tags: &[String],
) -> Vec<SuggestionItem> {
    // If a product is already selected, restrict to the same group (comparable products)
    let allowed_group: Option<u64> = selected_tags.iter().find_map(|t| product_groups.get(t).copied());

    let filtered_products: Vec<&String> = products
        .iter()
        .filter(|p| {
            if let Some(grp) = allowed_group {
                product_groups.get(*p).copied() == Some(grp)
            } else {
                true
            }
        })
        .collect();

    if query.is_empty() {
        return filtered_products
            .into_iter()
            .map(|p| SuggestionItem {
                value: p.clone(),
                display: p.clone(),
                reason: String::new(),
                is_semantic: false,
                already_selected: selected_tags.iter().any(|t| t == p),
            })
            .collect();
    }

    let matcher = SkimMatcherV2::default();
    let q = query.to_lowercase();

    let mut scored: Vec<(SuggestionItem, i64)> = filtered_products
        .iter()
        .filter_map(|product| {
            let id_lower = product.to_lowercase();
            let mut score: i64 = 0;
            let mut reason = String::new();
            let mut is_semantic = false;

            if id_lower == q {
                score += 1000;
                reason = "exact id".to_string();
            }
            if id_lower.starts_with(&q) {
                score += 500;
                if reason.is_empty() {
                    reason = "id prefix".to_string();
                }
            }
            if id_lower.contains(&q) {
                score += 200;
                if reason.is_empty() {
                    reason = "id contains".to_string();
                }
            }
            if let Some(fs) = matcher.fuzzy_match(&id_lower, &q) {
                let capped = fs.min(150);
                if capped > 0 {
                    score += capped;
                    if reason.is_empty() {
                        reason = "fuzzy".to_string();
                    }
                }
            }

            // Alias & Category Match
            let mut alias_hit = false;
            let product_categories = PRODUCT_TO_CATEGORIES.get(&id_lower).cloned().unwrap_or_default();
            let query_categories = PRODUCT_TO_CATEGORIES.get(&q).cloned().unwrap_or_default();

            for pc in &product_categories {
                if q == *pc || query_categories.contains(pc) {
                    score += 540;
                    reason = format!("{}:{}", q, pc);
                    is_semantic = true;
                    alias_hit = true;
                    break;
                }
            }

            if !alias_hit {
                if let Some(category) = infer_category_from_attrs(product, attribute_values) {
                    if q == category || query_categories.contains(&category) {
                        score += 520;
                        reason = format!("category:{}", category);
                        is_semantic = true;
                    }
                }
            }

            if score > 0 {
                Some((
                    SuggestionItem {
                        value: (*product).clone(),
                        display: (*product).clone(),
                        reason,
                        is_semantic,
                        already_selected: selected_tags.iter().any(|t| t == *product),
                    },
                    score,
                ))
            } else {
                None
            }
        })
        .collect();

    scored.sort_by(|a, b| {
        b.0.is_semantic
            .cmp(&a.0.is_semantic)
            .then(b.1.cmp(&a.1))
    });
    scored.into_iter().map(|(item, _)| item).collect()
}

/// Substring filter for regions, respecting product-region associations.
/// Each region's `reason` shows the provider(s) it belongs to (first word of the product key).
pub fn suggest_regions(
    query: &str,
    all_regions: &[String],
    product_regions: &HashMap<String, Vec<String>>,
    product_tags: &[String],
    selected_tags: &[String],
) -> Vec<SuggestionItem> {
    let q = query.to_lowercase();

    // Build a map from region -> set of providers that have it.
    // Product key format: "<provider> <product...>", first token is provider.
    let mut region_providers: HashMap<String, std::collections::BTreeSet<String>> = HashMap::new();
    for (product_key, regs) in product_regions {
        let provider = product_key
            .split_whitespace()
            .next()
            .unwrap_or(product_key)
            .to_string();
        for r in regs {
            if !r.is_empty() {
                region_providers
                    .entry(r.clone())
                    .or_default()
                    .insert(provider.clone());
            }
        }
    }

    let regions: Vec<String> = if product_tags.is_empty() {
        let mut v = all_regions.to_vec();
        v.sort();
        v
    } else {
        let mut aggregated = std::collections::HashSet::new();
        for p in product_tags {
            if let Some(regs) = product_regions.get(&p.to_lowercase()) {
                for r in regs {
                    if !r.is_empty() {
                        aggregated.insert(r.clone());
                    }
                }
            }
        }
        if aggregated.is_empty() {
            let mut v = all_regions.to_vec();
            v.sort();
            v
        } else {
            let mut v: Vec<_> = aggregated.into_iter().collect();
            v.sort();
            v
        }
    };

    regions
        .into_iter()
        .filter(|r| q.is_empty() || r.to_lowercase().contains(&q))
        .map(|r| {
            let reason = region_providers
                .get(&r)
                .map(|providers| providers.iter().cloned().collect::<Vec<_>>().join(","))
                .unwrap_or_default();
            SuggestionItem {
                value: r.clone(),
                display: r.clone(),
                reason,
                is_semantic: false,
                already_selected: selected_tags.iter().any(|t| *t == r),
            }
        })
        .collect()
}

/// Suggest attribute keys and values for a given product in two phases:
///
/// **Phase 1 – Key selection** (query has no `=`):
///   - Uses `product_attrs` (key names only) for fast key listing.
///   - Selecting a key sets search_input to "key=" to enter phase 2.
///
/// **Phase 2 – Value selection** (query contains `=`):
///   - Uses `attribute_values` to show values for the matched key.
///   - `value` = `key=value` (the full tag string ready to add).
pub fn suggest_attrs(
    query: &str,
    product_ids: &[String],
    product_attrs: &HashMap<String, Vec<String>>,
    attribute_values: &HashMap<String, HashMap<String, Vec<String>>>,
    selected_tags: &[String],
) -> Vec<SuggestionItem> {
    let mut suggestions: Vec<SuggestionItem> = Vec::new();
    let q = query.to_lowercase();

    if q.contains('=') {
        // Phase 2: value selection — use attribute_values
        let mut parts = q.splitn(2, '=');
        let key_filter = parts.next().unwrap_or("").to_string();
        let val_filter = parts.next().unwrap_or("").to_string();

        // Aggregate values across all selected products for this key
        let mut values_set: std::collections::HashSet<String> = std::collections::HashSet::new();
        for product_id in product_ids {
            if let Some(attrs) = attribute_values.get(product_id) {
                for (attr_name, vals) in attrs {
                    if attr_name.to_lowercase() == key_filter {
                        for v in vals {
                            values_set.insert(v.clone());
                        }
                    }
                }
            }
        }

        let mut values: Vec<String> = values_set.into_iter().collect();
        // Smart sort: if all values parse as numbers, sort numerically; otherwise alphabetically
        let all_numeric = values.iter().all(|v| v.trim().parse::<f64>().is_ok());
        if all_numeric {
            values.sort_by(|a, b| {
                let na = a.trim().parse::<f64>().unwrap_or(0.0);
                let nb = b.trim().parse::<f64>().unwrap_or(0.0);
                na.partial_cmp(&nb).unwrap_or(std::cmp::Ordering::Equal)
            });
        } else {
            values.sort_by(|a, b| a.to_lowercase().cmp(&b.to_lowercase()));
        }
        for val in values {
            if val_filter.is_empty() || val.to_lowercase().contains(val_filter.as_str()) {
                let tag_val = format!("{}={}", key_filter, val);
                suggestions.push(SuggestionItem {
                    already_selected: selected_tags.contains(&tag_val),
                    value: tag_val,
                    display: val,
                    reason: key_filter.clone(),
                    is_semantic: false,
                });
            }
        }
    } else {
        // Phase 1: key selection — use product_attrs (just key names)
        let mut keys: std::collections::HashSet<String> = std::collections::HashSet::new();
        for product_id in product_ids {
            if let Some(attr_keys) = product_attrs.get(product_id) {
                for k in attr_keys {
                    keys.insert(k.clone());
                }
            }
        }
        let mut keys: Vec<String> = keys.into_iter().collect();
        keys.sort();

        for attr_name in keys {
            if q.is_empty() || attr_name.to_lowercase().contains(q.as_str()) {
                // Count values from attribute_values for the reason hint
                let val_count: usize = product_ids.iter().filter_map(|pid| {
                    attribute_values.get(pid)?.get(&attr_name).map(|v| v.len())
                }).sum();
                suggestions.push(SuggestionItem {
                    already_selected: false,
                    value: attr_name.clone(),
                    display: attr_name,
                    reason: if val_count > 0 { format!("{} values", val_count) } else { String::new() },
                    is_semantic: false,
                });
            }
        }
    }

    suggestions
}
