package service

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"fsjson/internal/domain/model"
)

// SearchParams — структура фильтров поиска
type SearchParams struct {
	Query     string
	Path      string
	Type      string
	SizeCmp   map[string]int64 // gt/gte/lt/lte/eq
	Created   map[string]time.Time
	Modified  map[string]time.Time
	Recursive bool
	Limit     int
	Offset    int
}

// SearchResult — результат поиска
type SearchResult struct {
	FullPathOrig string
	SizeBytes    int64
	FileType     string
	Modified     time.Time
	Created      time.Time
}

// SearchFiles — выполняет поиск в дереве
func SearchFiles(root *model.FileInfo, params SearchParams) []SearchResult {
	results := []SearchResult{}
	var regex *regexp.Regexp

	if params.Query != "" {
		regex = wildcardToRegex(params.Query)
	}
	startPath := strings.TrimSuffix(params.Path, string(filepath.Separator))

	var walk func(node *model.FileInfo)
	walk = func(node *model.FileInfo) {
		// если задан path — ищем только в нужном подкаталоге
		if startPath != "" && !strings.HasPrefix(node.FullPath, startPath) {
			return
		}

		// фильтруем сам элемент
		if matchNode(node, params, regex) {
			results = append(results, SearchResult{
				FullPathOrig: node.FullPathOrig,
				SizeBytes:    node.SizeBytes,
				FileType:     node.FileType,
				Modified:     node.Updated,
				Created:      node.Created,
			})
		}

		// рекурсивный обход
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
		return []SearchResult{}
	}
	end := len(results)
	if params.Limit > 0 && start+params.Limit < end {
		end = start + params.Limit
	}

	return results[start:end]
}

// matchNode — проверка совпадения элемента с фильтрами
func matchNode(n *model.FileInfo, p SearchParams, re *regexp.Regexp) bool {
	// query
	if re != nil && !re.MatchString(strings.ToLower(n.FullName)) {
		return false
	}

	// type
	if p.Type != "" && n.FileType != p.Type {
		return false
	}

	// size
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
		}
	}

	// created/modified диапазоны (если заданы)
	for op, t := range p.Created {
		switch op {
		case "gt":
			if !n.Created.After(t) {
				return false
			}
		case "lt":
			if !n.Created.Before(t) {
				return false
			}
		}
	}
	for op, t := range p.Modified {
		switch op {
		case "gt":
			if !n.Updated.After(t) {
				return false
			}
		case "lt":
			if !n.Updated.Before(t) {
				return false
			}
		}
	}

	return true
}

// wildcardToRegex превращает шаблон с * и ? в regex
func wildcardToRegex(q string) *regexp.Regexp {
	q = strings.ToLower(q)
	q = strings.ReplaceAll(q, ".", "\\.")
	q = strings.ReplaceAll(q, "*", ".*")
	q = strings.ReplaceAll(q, "?", ".")
	re := regexp.MustCompile(q)
	return re
}
