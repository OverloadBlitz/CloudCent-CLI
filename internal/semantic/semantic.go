package semantic

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sahilm/fuzzy"
)

// productAliases maps generic terms to product ID keywords.
var productAliases = map[string][]string{
	"compute":   {"ec2", "compute engine", "virtual machine", "vm", "ecs", "lambda", "cloud functions", "app engine", "instances", "batch", "mac_compute", "bare_metal"},
	"vm":        {"ec2", "compute engine", "virtual machine", "instances", "vps"},
	"storage":   {"s3", "cloud storage", "blob storage", "ebs", "efs", "glacier", "object storage", "archive_storage", "backup_storage", "file_storage"},
	"database":  {"rds", "dynamodb", "cloud sql", "cosmos", "aurora", "redshift", "firestore", "bigtable", "spanner", "mysql", "postgres", "alloydb", "distributed_sql", "nosql_instance", "rds_instance", "timeseries_db"},
	"db":        {"rds", "dynamodb", "cloud sql", "cosmos", "aurora", "redshift", "firestore", "alloydb", "nosql"},
	"cdn":       {"cloudfront", "cloud cdn", "azure cdn", "media", "edge"},
	"serverless": {"lambda", "cloud functions", "azure functions", "fargate", "cloud run", "app service", "app_hosting"},
	"container": {"ecs", "eks", "gke", "aks", "fargate", "cloud run", "kubernetes"},
	"k8s":       {"eks", "gke", "aks", "kubernetes"},
	"cache":     {"elasticache", "memorystore", "redis", "memcached", "cache_instance"},
	"queue":     {"sqs", "pub/sub", "service bus", "eventbridge", "sns", "mq", "messaging"},
	"network":   {"vpc", "nat", "load balancer", "elb", "alb", "nlb", "route 53", "dns", "firewall", "ip_address", "nat_gateway"},
	"dns":       {"route 53", "cloud dns", "route53"},
	"lb":        {"elb", "alb", "nlb", "load balancer", "cloud load balancing"},
	"monitor":   {"cloudwatch", "cloud monitoring", "azure monitor", "stackdriver", "metrics"},
	"logging":   {"cloudwatch", "cloud logging", "log analytics", "stackdriver"},
	"ml":        {"sagemaker", "vertex ai", "azure ml", "machine learning", "ai", "ccai"},
	"ai":        {"sagemaker", "vertex ai", "azure ml", "cognitive", "rekognition", "machine learning", "ccai"},
	"gpu":       {"ec2", "compute engine", "virtual machine", "accelerator", "gpu", "compute_gpu"},
	"disk":      {"ebs", "persistent disk", "managed disk", "volume", "block", "compute_storage"},
	"email":     {"ses", "email", "communication services"},
	"secret":    {"secrets manager", "secret manager", "key vault", "kms"},
	"auth":      {"cognito", "identity platform", "azure ad", "iam", "identity"},
	"function":  {"lambda", "cloud functions", "azure functions", "cloud run"},
}

// buildProductToCategories builds the inverted alias map.
var productToCategories = func() map[string][]string {
	m := map[string][]string{}
	for category, aliases := range productAliases {
		m[category] = append(m[category], category)
		for _, alias := range aliases {
			m[alias] = append(m[alias], category)
		}
	}
	return m
}()

var attrCategoryHints = [][2]string{
	{"instancetype", "compute"}, {"instance_type", "compute"}, {"machinetype", "compute"},
	{"machine_type", "compute"}, {"vcpu", "compute"}, {"memory", "compute"},
	{"storagetype", "storage"}, {"storage_type", "storage"}, {"storageclass", "storage"},
	{"storage_class", "storage"}, {"volumetype", "storage"}, {"volume_type", "storage"},
	{"databaseengine", "database"}, {"database_engine", "database"}, {"engineversion", "database"},
	{"cacheengine", "cache"}, {"nodetype", "cache"}, {"protocol", "network"},
}

// SuggestionItem is a single entry in a suggestion list.
type SuggestionItem struct {
	Value           string
	Display         string
	Reason          string
	IsSemantic      bool
	AlreadySelected bool
}

func inferCategoryFromAttrs(productID string, attributeValues map[string]map[string][]string) string {
	attrs, ok := attributeValues[productID]
	if !ok {
		return ""
	}
	for key := range attrs {
		lower := strings.ToLower(key)
		for _, hint := range attrCategoryHints {
			if strings.Contains(lower, hint[0]) {
				return hint[1]
			}
		}
	}
	return ""
}

// ScoreAndSuggestProducts returns a scored, ranked list of product suggestions.
func ScoreAndSuggestProducts(
	query string,
	products []string,
	attributeValues map[string]map[string][]string,
	productGroups map[string]uint64,
	selectedTags []string,
) []SuggestionItem {
	// Find allowed group from selected tags
	var allowedGroup *uint64
	for _, t := range selectedTags {
		if g, ok := productGroups[t]; ok {
			g := g
			allowedGroup = &g
			break
		}
	}

	filtered := make([]string, 0, len(products))
	for _, p := range products {
		if allowedGroup != nil {
			if g, ok := productGroups[p]; !ok || g != *allowedGroup {
				continue
			}
		}
		filtered = append(filtered, p)
	}

	if query == "" {
		items := make([]SuggestionItem, 0, len(filtered))
		for _, p := range filtered {
			already := false
			for _, t := range selectedTags {
				if t == p {
					already = true
					break
				}
			}
			items = append(items, SuggestionItem{Value: p, Display: p, AlreadySelected: already})
		}
		return items
	}

	q := strings.ToLower(query)

	type scored struct {
		item  SuggestionItem
		score int64
	}
	var result []scored

	fuzzyMatches := fuzzy.Find(q, filtered)
	fuzzyScores := map[string]int{}
	for _, m := range fuzzyMatches {
		fuzzyScores[filtered[m.Index]] = m.Score
	}

	for _, product := range filtered {
		idLower := strings.ToLower(product)
		var score int64
		reason := ""
		isSemantic := false

		if idLower == q {
			score += 1000
			reason = "exact id"
		}
		if strings.HasPrefix(idLower, q) {
			score += 500
			if reason == "" {
				reason = "id prefix"
			}
		}
		if strings.Contains(idLower, q) {
			score += 200
			if reason == "" {
				reason = "id contains"
			}
		}
		if fs, ok := fuzzyScores[product]; ok {
			capped := int64(fs)
			if capped > 150 {
				capped = 150
			}
			if capped > 0 {
				score += capped
				if reason == "" {
					reason = "fuzzy"
				}
			}
		}

		// Alias/category matching
		productCats := productToCategories[idLower]
		queryCats := productToCategories[q]
		aliasHit := false
		for _, pc := range productCats {
			if q == pc {
				score += 540
				reason = q + ":" + pc
				isSemantic = true
				aliasHit = true
				break
			}
			for _, qc := range queryCats {
				if qc == pc {
					score += 540
					reason = q + ":" + pc
					isSemantic = true
					aliasHit = true
					break
				}
			}
			if aliasHit {
				break
			}
		}

		if !aliasHit {
			if cat := inferCategoryFromAttrs(product, attributeValues); cat != "" {
				if q == cat {
					score += 520
					reason = "category:" + cat
					isSemantic = true
				} else {
					for _, qc := range queryCats {
						if qc == cat {
							score += 520
							reason = "category:" + cat
							isSemantic = true
							break
						}
					}
				}
			}
		}

		if score > 0 {
			already := false
			for _, t := range selectedTags {
				if t == product {
					already = true
					break
				}
			}
			result = append(result, scored{
				item: SuggestionItem{
					Value: product, Display: product, Reason: reason,
					IsSemantic: isSemantic, AlreadySelected: already,
				},
				score: score,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].item.IsSemantic != result[j].item.IsSemantic {
			return result[i].item.IsSemantic
		}
		return result[i].score > result[j].score
	})

	items := make([]SuggestionItem, 0, len(result))
	for _, r := range result {
		items = append(items, r.item)
	}
	return items
}

// SuggestRegions returns filtered region suggestions.
func SuggestRegions(
	query string,
	allRegions []string,
	productRegions map[string][]string,
	productTags []string,
	selectedTags []string,
) []SuggestionItem {
	q := strings.ToLower(query)

	// Build region->providers map
	regionProviders := map[string]map[string]struct{}{}
	for productKey, regs := range productRegions {
		parts := strings.Fields(productKey)
		provider := productKey
		if len(parts) > 0 {
			provider = parts[0]
		}
		for _, r := range regs {
			if r == "" {
				continue
			}
			if regionProviders[r] == nil {
				regionProviders[r] = map[string]struct{}{}
			}
			regionProviders[r][provider] = struct{}{}
		}
	}

	var regions []string
	if len(productTags) == 0 {
		regions = append([]string{}, allRegions...)
		sort.Strings(regions)
	} else {
		seen := map[string]struct{}{}
		for _, p := range productTags {
			if regs, ok := productRegions[strings.ToLower(p)]; ok {
				for _, r := range regs {
					if r != "" {
						seen[r] = struct{}{}
					}
				}
			}
		}
		if len(seen) == 0 {
			regions = append([]string{}, allRegions...)
		} else {
			for r := range seen {
				regions = append(regions, r)
			}
		}
		sort.Strings(regions)
	}

	var items []SuggestionItem
	for _, r := range regions {
		if q != "" && !strings.Contains(strings.ToLower(r), q) {
			continue
		}
		provs := []string{}
		for p := range regionProviders[r] {
			provs = append(provs, p)
		}
		sort.Strings(provs)
		already := false
		for _, t := range selectedTags {
			if t == r {
				already = true
				break
			}
		}
		items = append(items, SuggestionItem{
			Value: r, Display: r, Reason: strings.Join(provs, ","),
			AlreadySelected: already,
		})
	}
	return items
}

// SuggestAttrs returns attribute key or key=value suggestions.
func SuggestAttrs(
	query string,
	productIDs []string,
	productAttrs map[string][]string,
	attributeValues map[string]map[string][]string,
	selectedTags []string,
) []SuggestionItem {
	q := strings.ToLower(query)

	if strings.Contains(q, "=") {
		// Phase 2: value selection
		parts := strings.SplitN(q, "=", 2)
		keyFilter := parts[0]
		valFilter := parts[1]

		valSet := map[string]struct{}{}
		for _, pid := range productIDs {
			if attrs, ok := attributeValues[pid]; ok {
				for attrName, vals := range attrs {
					if strings.ToLower(attrName) == keyFilter {
						for _, v := range vals {
							valSet[v] = struct{}{}
						}
					}
				}
			}
		}

		values := make([]string, 0, len(valSet))
		for v := range valSet {
			values = append(values, v)
		}

		allNumeric := len(values) > 0
		for _, v := range values {
			if _, err := strconv.ParseFloat(v, 64); err != nil {
				allNumeric = false
				break
			}
		}
		if allNumeric {
			sort.Slice(values, func(i, j int) bool {
				a, _ := strconv.ParseFloat(values[i], 64)
				b, _ := strconv.ParseFloat(values[j], 64)
				return a < b
			})
		} else {
			sort.Slice(values, func(i, j int) bool {
				return strings.ToLower(values[i]) < strings.ToLower(values[j])
			})
		}

		var items []SuggestionItem
		for _, val := range values {
			if valFilter != "" && !strings.Contains(strings.ToLower(val), valFilter) {
				continue
			}
			tagVal := keyFilter + "=" + val
			already := false
			for _, t := range selectedTags {
				if t == tagVal {
					already = true
					break
				}
			}
			items = append(items, SuggestionItem{
				Value: tagVal, Display: val, Reason: keyFilter, AlreadySelected: already,
			})
		}
		return items
	}

	// Phase 1: key selection
	keySet := map[string]struct{}{}
	for _, pid := range productIDs {
		if keys, ok := productAttrs[pid]; ok {
			for _, k := range keys {
				keySet[k] = struct{}{}
			}
		}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var items []SuggestionItem
	for _, attrName := range keys {
		if q != "" && !strings.Contains(strings.ToLower(attrName), q) {
			continue
		}
		valCount := 0
		for _, pid := range productIDs {
			if attrs, ok := attributeValues[pid]; ok {
				if vals, ok := attrs[attrName]; ok {
					valCount += len(vals)
				}
			}
		}
		reason := ""
		if valCount > 0 {
			reason = fmt.Sprintf("%d values", valCount)
		}
		items = append(items, SuggestionItem{
			Value: attrName, Display: attrName, Reason: reason,
		})
	}
	return items
}

