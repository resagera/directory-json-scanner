package service

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fsjson/internal/domain/model"
)

// ProcessPath анализирует один путь
// ProcessPath — версия по умолчанию (без внешнего I/O лимита)
func ProcessPath(path string, info os.FileInfo, skipMd5 bool) model.FileInfo {
	return ProcessPathWith(path, info, skipMd5,
		func(dir string) int {
			list, _ := os.ReadDir(dir)
			return len(list)
		},
		func(p string) string {
			return FileMD5(p)
		},
	)
}

// AssembleNestedFromFlat собирает дерево из flat-массива
func AssembleNestedFromFlat(flat []model.FileInfo) model.FileInfo {
	if len(flat) == 0 {
		return model.FileInfo{IsDir: true, FullName: "(empty)", NameOnly: "(empty)"}
	}

	type nodePtr = *model.FileInfo
	pathToNode := make(map[string]nodePtr, len(flat))
	parentToKids := make(map[string][]model.FileInfo, len(flat))

	for i := range flat {
		if flat[i].ParentDir == "." {
			flat[i].ParentDir = ""
		}
		pathToNode[flat[i].FullPath] = &flat[i]
	}

	var roots []model.FileInfo
	for _, fi := range flat {
		if _, ok := pathToNode[fi.ParentDir]; ok {
			parentToKids[fi.ParentDir] = append(parentToKids[fi.ParentDir], fi)
		} else {
			roots = append(roots, fi)
		}
	}

	var build func(model.FileInfo) model.FileInfo
	build = func(n model.FileInfo) model.FileInfo {
		kids := parentToKids[n.FullPath]
		if len(kids) == 0 {
			return n
		}
		n.Children = make([]model.FileInfo, 0, len(kids))
		var total int64
		for _, ch := range kids {
			b := build(ch)
			n.Children = append(n.Children, b)
			total += b.SizeBytes
		}
		if n.IsDir {
			n.SizeBytes = total
			n.SizeHuman = HumanSize(total)
			sort.Slice(n.Children, func(i, j int) bool {
				di, dj := n.Children[i].IsDir, n.Children[j].IsDir
				if di != dj {
					return di && !dj
				}
				return strings.ToLower(n.Children[i].FullName) < strings.ToLower(n.Children[j].FullName)
			})
		}
		return n
	}

	if len(roots) == 1 {
		return build(roots[0])
	}
	return model.FileInfo{
		IsDir:      true,
		FullName:   "(root)",
		NameOnly:   "(root)",
		FullPath:   "",
		Children:   roots,
		SizeBytes:  0,
		SizeHuman:  "",
		ChildCount: len(roots),
	}
}

// ComputeDirSizes пересчитывает размеры и даты рекурсивно
func ComputeDirSizes(node *model.FileInfo) int64 {
	if !node.IsDir {
		return node.SizeBytes
	}
	var total int64
	var earliest, latest time.Time
	for i := range node.Children {
		sz := ComputeDirSizes(&node.Children[i])
		total += sz
		c := node.Children[i]
		if !c.Created.IsZero() && (earliest.IsZero() || c.Created.Before(earliest)) {
			earliest = c.Created
		}
		if !c.Updated.IsZero() && (latest.IsZero() || c.Updated.After(latest)) {
			latest = c.Updated
		}
	}
	node.SizeBytes = total
	node.SizeHuman = HumanSize(total)
	if !earliest.IsZero() {
		node.Created = earliest
	}
	if !latest.IsZero() {
		node.Updated = latest
	}
	if node.Md5 == "" {
		node.Md5 = Md5String(node.FullName)
	}
	return total
}

// ProcessPathWith — как ProcessPath, но с инъекцией I/O-функций (для лимита)
func ProcessPathWith(
	path string,
	info os.FileInfo,
	skipMd5 bool,
	readDirCount func(dir string) int,
	fileMD5 func(path string) string,
) model.FileInfo {
	parent := filepath.Dir(path)
	if parent == "." {
		parent = ""
	}

	size := int64(0)
	if !info.IsDir() {
		size = info.Size()
	}

	entry := model.FileInfo{
		IsDir:        info.IsDir(),
		FullName:     info.Name(),
		Ext:          strings.TrimPrefix(strings.ToLower(filepath.Ext(info.Name())), "."),
		NameOnly:     strings.TrimSuffix(info.Name(), filepath.Ext(info.Name())),
		SizeBytes:    size,
		SizeHuman:    HumanSize(size),
		FullPath:     path,
		FullPathOrig: path,
		ParentDir:    parent,
		Created:      info.ModTime(),
		Updated:      info.ModTime(),
		Perm:         info.Mode().String(),
		FileType:     DetectFileType(info.Name()),
	}

	if info.IsDir() {
		if readDirCount != nil {
			entry.ChildCount = readDirCount(path)
		}
		if !skipMd5 {
			entry.Md5 = Md5String(info.Name())
		}
	} else if !skipMd5 && fileMD5 != nil {
		entry.Md5 = fileMD5(path)
	}

	return entry
}

// ShouldExclude — проверка по подстроке ПОЛНОГО пути (регистронезависимо)
func ShouldExclude(absPath string, excludes []string) bool {
	pl := strings.ToLower(absPath)
	for _, ex := range excludes {
		if ex != "" && strings.Contains(pl, ex) {
			return true
		}
	}
	return false
}

// --- MD5 helpers (чистые, без инфраструктурных зависимостей) ---
func Md5String(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func FileMD5(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
