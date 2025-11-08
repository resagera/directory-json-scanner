package service

import (
	"fmt"
	"path/filepath"
	"strings"

	"fsjson/internal/domain/model"
)

// HumanSize возвращает человекочитаемый размер файла
func HumanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	value := float64(size) / float64(div)
	suffixes := []string{"KB", "MB", "GB", "TB", "PB", "EB"}
	if exp >= len(suffixes) {
		exp = len(suffixes) - 1
	}
	return fmt.Sprintf("%.2f %s", value, suffixes[exp])
}

// DetectFileType возвращает категорию файла по расширению
func DetectFileType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tiff":
		return "image"
	case ".mp4", ".avi", ".mkv", ".mov", ".webm":
		return "video"
	case ".mp3", ".wav", ".flac", ".aac", ".ogg":
		return "audio"
	case ".txt", ".md", ".log", ".csv":
		return "text"
	case ".go", ".js", ".ts", ".py", ".html", ".css", ".json", ".yaml", ".yml",
		".rs", ".java", ".c", ".cpp", ".cs", ".php", ".sh":
		return "code"
	default:
		return "other"
	}
}

// AppendFlatUnique добавляет элементы с проверкой дубликатов по FullPathOrig
func AppendFlatUnique(dst, src []model.FileInfo, seen map[string]struct{}) []model.FileInfo {
	if seen == nil {
		return append(dst, src...)
	}
	for _, f := range src {
		if _, ok := seen[f.FullPathOrig]; ok {
			continue
		}
		seen[f.FullPathOrig] = struct{}{}
		dst = append(dst, f)
	}
	return dst
}

// FlattenTree превращает дерево в flat []FileInfo
func FlattenTree(root model.FileInfo) []model.FileInfo {
	var flat []model.FileInfo
	var walk func(model.FileInfo)
	walk = func(node model.FileInfo) {
		flat = append(flat, node)
		for _, c := range node.Children {
			walk(c)
		}
	}
	walk(root)
	return flat
}

// RecountChildCounts рекурсивно пересчитывает количество потомков у директорий
func RecountChildCounts(node *model.FileInfo) int {
	if !node.IsDir {
		node.ChildCount = 0
		return 0
	}
	node.ChildCount = len(node.Children)
	for i := range node.Children {
		RecountChildCounts(&node.Children[i])
	}
	return node.ChildCount
}
