package service

import (
	"testing"

	"fsjson/internal/domain/model"
)

// helper для построения тестовых деревьев
func dir(name string, children ...model.FileInfo) model.FileInfo {
	d := model.FileInfo{
		IsDir:    true,
		FullName: name,
		NameOnly: name,
		FullPath: "/" + name,
		Children: children,
	}
	var total int64
	for _, c := range children {
		total += c.SizeBytes
	}
	d.SizeBytes = total
	d.ChildCount = len(children)
	return d
}

func file(name string, size int64) model.FileInfo {
	return model.FileInfo{
		IsDir:     false,
		FullName:  name,
		NameOnly:  name,
		FullPath:  "/" + name,
		SizeBytes: size,
	}
}

func TestMergeChildren_DuplicateFilesAndDirs(t *testing.T) {
	// === дерево 1 ===
	tree1 := dir("RootA",
		dir("level1",
			dir("shared_dir",
				file("same.txt", 100),
				file("uniqueA.txt", 50),
			),
			file("rootA_only.txt", 20),
		),
	)

	// === дерево 2 ===
	tree2 := dir("RootB",
		dir("level1",
			dir("shared_dir",
				file("same.txt", 100),
				file("uniqueB.txt", 75),
			),
			file("rootB_only.txt", 40),
		),
	)

	// --- merge с dedupe=false ---
	merged := MergeRootChildren([]model.FileInfo{tree1, tree2}, false)

	shared := findDirByPath(&merged, "shared_dir")
	if shared == nil {
		t.Fatal("shared_dir не найден в объединённом дереве")
	}

	if len(shared.Children) != 4 {
		t.Fatalf("ожидалось 4 файла в shared_dir при dedupe=false, получено %d", len(shared.Children))
	}

	// --- merge с dedupe=true ---
	mergedDedupe := MergeRootChildren([]model.FileInfo{tree1, tree2}, true)
	shared2 := findDirByPath(&mergedDedupe, "shared_dir")
	if shared2 == nil {
		t.Fatal("shared_dir не найден в dedupe=true дереве")
	}

	if len(shared2.Children) != 3 {
		t.Fatalf("ожидалось 3 файла в shared_dir при dedupe=true, получено %d", len(shared2.Children))
	}
}

// поиск директории по имени (рекурсивно)
func findDirByPath(node *model.FileInfo, target string) *model.FileInfo {
	if node.IsDir && node.FullName == target {
		return node
	}
	for i := range node.Children {
		if sub := findDirByPath(&node.Children[i], target); sub != nil {
			return sub
		}
	}
	return nil
}

// makeDeepTree генерирует фиктивную иерархию для теста
func makeDeepTree(rootName string, withExtra bool) model.FileInfo {
	root := model.FileInfo{
		IsDir:      true,
		FullName:   rootName,
		NameOnly:   rootName,
		FullPath:   rootName,
		Children:   []model.FileInfo{},
		ChildCount: 1,
	}

	// Уровень 2
	level2 := model.FileInfo{
		IsDir:      true,
		FullName:   "level2",
		NameOnly:   "level2",
		FullPath:   rootName + "/level2",
		Children:   []model.FileInfo{},
		ChildCount: 1,
	}

	// Уровень 3 (одинаковое имя для обеих структур)
	level3 := model.FileInfo{
		IsDir:      true,
		FullName:   "shared_dir",
		NameOnly:   "shared_dir",
		FullPath:   rootName + "/level2/shared_dir",
		Children:   []model.FileInfo{},
		ChildCount: 1,
	}

	// Уровень 4 — вложенные файлы
	level3.Children = append(level3.Children, model.FileInfo{
		IsDir: false, FullName: "file_common.txt", SizeBytes: 10, FullPath: level3.FullPath + "/file_common.txt",
	})
	if withExtra {
		level3.Children = append(level3.Children, model.FileInfo{
			IsDir: false, FullName: "unique_b.txt", SizeBytes: 5, FullPath: level3.FullPath + "/unique_b.txt",
		})
	} else {
		level3.Children = append(level3.Children, model.FileInfo{
			IsDir: false, FullName: "unique_a.txt", SizeBytes: 7, FullPath: level3.FullPath + "/unique_a.txt",
		})
	}

	level2.Children = append(level2.Children, level3)
	root.Children = append(root.Children, level2)
	return root
}
