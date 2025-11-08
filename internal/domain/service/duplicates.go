package service

import (
	"sort"

	"fsjson/internal/domain/model"
)

// DuplicateGroup — группа файлов с одинаковым MD5
type DuplicateGroup struct {
	Md5   string   `json:"md5"`
	Files []string `json:"files"`
	Count int      `json:"count"`
	Size  int64    `json:"size"`
}

// DuplicatesResponse — результат поиска дубликатов
type DuplicatesResponse struct {
	Groups []DuplicateGroup `json:"groups"`
	Total  int              `json:"total_groups"`
	Files  int              `json:"total_files"`
}

// FindDuplicates — ищет все файлы с одинаковым MD5
func FindDuplicates(root *model.FileInfo) DuplicatesResponse {
	md5map := make(map[string][]*model.FileInfo)

	var walk func(n *model.FileInfo)
	walk = func(n *model.FileInfo) {
		if n == nil {
			return
		}
		if !n.IsDir && n.Md5 != "" {
			md5map[n.Md5] = append(md5map[n.Md5], n)
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(root)

	groups := make([]DuplicateGroup, 0, len(md5map))
	totalFiles := 0
	for md5, files := range md5map {
		if len(files) > 1 { // только дубликаты
			group := DuplicateGroup{Md5: md5, Count: len(files)}
			for _, f := range files {
				group.Files = append(group.Files, f.FullPathOrig)
				group.Size += f.SizeBytes
			}
			totalFiles += len(files)
			groups = append(groups, group)
		}
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count == groups[j].Count {
			return groups[i].Size > groups[j].Size
		}
		return groups[i].Count > groups[j].Count
	})

	return DuplicatesResponse{
		Groups: groups,
		Total:  len(groups),
		Files:  totalFiles,
	}
}
