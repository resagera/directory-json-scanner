package service

import (
	"sort"
	"strings"

	"fsjson/internal/domain/model"
)

// MergeDirectories рекурсивно объединяет содержимое двух директорий любой глубины.
// Каталоги с одинаковыми именами объединяются рекурсивно.
// Файлы с одинаковыми именами дублируются, если dedupe == false.
func MergeDirectories(a, b model.FileInfo, dedupe bool) model.FileInfo {
	result := a

	existing := make(map[string]*model.FileInfo, len(result.Children))
	for i := range result.Children {
		existing[result.Children[i].FullName] = &result.Children[i]
	}

	for _, ch := range b.Children {
		if ex, ok := existing[ch.FullName]; ok {
			if ch.IsDir && ex.IsDir {
				merged := MergeDirectories(*ex, ch, dedupe)
				*ex = merged
			} else if !ch.IsDir && !dedupe {
				result.Children = append(result.Children, ch)
			} else if !ch.IsDir && dedupe {
				// skip duplicate file
				continue
			}
		} else {
			result.Children = append(result.Children, ch)
			existing[ch.FullName] = &result.Children[len(result.Children)-1]
		}
	}

	// ⚠️ финальный проход для удаления возможных повторов по FullName (страховка)
	if dedupe {
		unique := make([]model.FileInfo, 0, len(result.Children))
		seen := make(map[string]bool)
		for _, ch := range result.Children {
			if seen[ch.FullName] {
				continue
			}
			seen[ch.FullName] = true
			unique = append(unique, ch)
		}
		result.Children = unique
	}

	// пересчёт размеров и сортировка
	var total int64
	for i := range result.Children {
		total += result.Children[i].SizeBytes
	}
	result.SizeBytes = total
	result.SizeHuman = HumanSize(total)
	result.ChildCount = len(result.Children)

	sort.Slice(result.Children, func(i, j int) bool {
		if result.Children[i].IsDir != result.Children[j].IsDir {
			return result.Children[i].IsDir
		}
		return strings.ToLower(result.Children[i].FullName) < strings.ToLower(result.Children[j].FullName)
	})
	return result
}

// MergeRootChildren объединяет содержимое корней разных файлов в один общий корень.
// Каталоги с одинаковыми именами всегда объединяются.
// Файлы с одинаковыми именами добавляются как дубликаты, если dedupe=false.
func MergeRootChildren(roots []model.FileInfo, dedupe bool) model.FileInfo {
	if len(roots) == 0 {
		return model.FileInfo{}
	}
	if len(roots) == 1 {
		return roots[0]
	}

	var names []string
	for _, r := range roots {
		if r.FullName != "" {
			names = append(names, r.FullName)
		}
	}
	rootName := strings.Join(names, "+")

	merged := roots[0]
	for i := 1; i < len(roots); i++ {
		merged = MergeDirectories(merged, roots[i], dedupe)
	}

	merged.FullName = rootName
	merged.NameOnly = rootName
	merged.FullPath = rootName
	merged.FileType = "merged"

	var total int64
	for _, c := range merged.Children {
		total += c.SizeBytes
	}
	merged.SizeBytes = total
	merged.SizeHuman = HumanSize(total)
	merged.ChildCount = len(merged.Children)
	return merged
}
