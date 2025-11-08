package service

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"fsjson/internal/domain/model"
)

// SearchParams — параметры фильтрации
type SearchParams struct {
	Query     string
	Path      string
	Types     []string
	SizeCmp   map[string]int64
	Created   map[string]time.Time
	Modified  map[string]time.Time
	Recursive bool
	Limit     int
	Offset    int
}

// SearchResult — один элемент результата
type SearchResult struct {
	FullPathOrig string    `json:"FullPathOrig"`
	SizeBytes    int64     `json:"SizeBytes"`
	FileType     string    `json:"FileType"`
	Modified     time.Time `json:"Modified"`
	Created      time.Time `json:"Created"`
}

// SearchStats — статистика по типам
type SearchStats map[string]int

// SearchResponse — итоговый ответ
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Stats   SearchStats    `json:"stats"`
	Total   int            `json:"total"`
}

// SearchFiles — основной алгоритм поиска
func SearchFiles(root *model.FileInfo, params SearchParams) SearchResponse {
	results := []SearchResult{}
	var regex *regexp.Regexp

	if params.Query != "" {
		regex = wildcardToRegex(params.Query)
	}

	startPath := strings.TrimSuffix(params.Path, string(filepath.Separator))

	typeSet := make(map[string]bool)
	for _, t := range params.Types {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			typeSet[t] = true
		}
	}

	var walk func(node *model.FileInfo)
	walk = func(node *model.FileInfo) {
		if startPath != "" && !strings.HasPrefix(node.FullPath, startPath) {
			return
		}

		if matchNode(node, params, regex, typeSet) {
			results = append(results, SearchResult{
				FullPathOrig: node.FullPathOrig,
				SizeBytes:    node.SizeBytes,
				FileType:     node.FileType,
				Modified:     node.Updated,
				Created:      node.Created,
			})
		}

		if node.IsDir && params.Recursive {
			for i := range node.Children {
				walk(&node.Children[i])
			}
		}
	}

	walk(root)

	// пагинация
	start := params.Offset
	if start > len(results) {
		return SearchResponse{Results: []SearchResult{}, Stats: SearchStats{}, Total: 0}
	}
	end := len(results)
	if params.Limit > 0 && start+params.Limit < end {
		end = start + params.Limit
	}
	results = results[start:end]

	stats := make(SearchStats)
	for _, r := range results {
		stats[r.FileType]++
	}

	return SearchResponse{
		Results: results,
		Stats:   stats,
		Total:   len(results),
	}
}

// matchNode — фильтрация узла по всем параметрам
func matchNode(n *model.FileInfo, p SearchParams, re *regexp.Regexp, typeSet map[string]bool) bool {
	// query
	if re != nil && !re.MatchString(strings.ToLower(n.FullName)) {
		return false
	}

	// type (множественный)
	if len(typeSet) > 0 && !typeSet[strings.ToLower(n.FileType)] {
		return false
	}

	// size (все операции включая between)
	for op, val := range p.SizeCmp {
		switch op {
		case "gt":
			if !(n.SizeBytes > val) {
				return false
			}
		case "gte":
			if !(n.SizeBytes >= val) {
				return false
			}
		case "lt":
			if !(n.SizeBytes < val) {
				return false
			}
		case "lte":
			if !(n.SizeBytes <= val) {
				return false
			}
		case "eq":
			if n.SizeBytes != val {
				return false
			}
		case "between":
			// диапазон задан как min,max
			min := p.SizeCmp["between_min"]
			max := p.SizeCmp["between_max"]
			if !(n.SizeBytes >= min && n.SizeBytes <= max) {
				return false
			}
		}
	}

	// created
	for op, t := range p.Created {
		switch op {
		case "gt":
			if !n.Created.After(t) {
				return false
			}
		case "gte":
			if n.Created.Before(t) {
				return false
			}
		case "lt":
			if !n.Created.Before(t) {
				return false
			}
		case "lte":
			if n.Created.After(t) {
				return false
			}
		}
	}

	// modified
	for op, t := range p.Modified {
		switch op {
		case "gt":
			if !n.Updated.After(t) {
				return false
			}
		case "gte":
			if n.Updated.Before(t) {
				return false
			}
		case "lt":
			if !n.Updated.Before(t) {
				return false
			}
		case "lte":
			if n.Updated.After(t) {
				return false
			}
		}
	}

	return true
}

// wildcardToRegex — поддержка шаблонов (*, ?)
func wildcardToRegex(q string) *regexp.Regexp {
	q = strings.ToLower(q)
	q = strings.ReplaceAll(q, ".", "\\.")
	q = strings.ReplaceAll(q, "*", ".*")
	q = strings.ReplaceAll(q, "?", ".")
	re := regexp.MustCompile(q)
	return re
}
