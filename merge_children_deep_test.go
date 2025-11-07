package main

import (
	"encoding/json"
	"os"
	"testing"
)

// makeDeepTree генерирует фиктивную иерархию для теста
func makeDeepTree(rootName string, withExtra bool) FileInfo {
	root := FileInfo{
		IsDir:      true,
		FullName:   rootName,
		NameOnly:   rootName,
		FullPath:   rootName,
		Children:   []FileInfo{},
		ChildCount: 1,
	}

	// Уровень 2
	level2 := FileInfo{
		IsDir:      true,
		FullName:   "level2",
		NameOnly:   "level2",
		FullPath:   rootName + "/level2",
		Children:   []FileInfo{},
		ChildCount: 1,
	}

	// Уровень 3 (одинаковое имя для обеих структур)
	level3 := FileInfo{
		IsDir:      true,
		FullName:   "shared_dir",
		NameOnly:   "shared_dir",
		FullPath:   rootName + "/level2/shared_dir",
		Children:   []FileInfo{},
		ChildCount: 1,
	}

	// Уровень 4 — вложенные файлы
	level3.Children = append(level3.Children, FileInfo{
		IsDir: false, FullName: "file_common.txt", SizeBytes: 10, FullPath: level3.FullPath + "/file_common.txt",
	})
	if withExtra {
		level3.Children = append(level3.Children, FileInfo{
			IsDir: false, FullName: "unique_b.txt", SizeBytes: 5, FullPath: level3.FullPath + "/unique_b.txt",
		})
	} else {
		level3.Children = append(level3.Children, FileInfo{
			IsDir: false, FullName: "unique_a.txt", SizeBytes: 7, FullPath: level3.FullPath + "/unique_a.txt",
		})
	}

	level2.Children = append(level2.Children, level3)
	root.Children = append(root.Children, level2)
	return root
}

func TestMergeChildren_DuplicateFilesAndDirs(t *testing.T) {
	// создаём два дерева: оба имеют shared_dir, но разный набор файлов внутри
	treeA := makeDeepTree("RootA", false)
	treeB := makeDeepTree("RootB", true)

	file1, _ := os.CreateTemp("", "deep1_*.json")
	file2, _ := os.CreateTemp("", "deep2_*.json")
	defer os.Remove(file1.Name())
	defer os.Remove(file2.Name())

	json.NewEncoder(file1).Encode(treeA)
	json.NewEncoder(file2).Encode(treeB)
	file1.Close()
	file2.Close()

	// общий setup
	*mergeFlag = file1.Name() + "," + file2.Name()
	*mergeChildrenFlag = true
	*outputFlag = "merged_deep.json"
	*mergeFlatFlag = false
	*prettyFlag = false

	runMerge := func(dedupe bool) FileInfo {
		*dedupeFlag = dedupe
		mergeMode()
		defer os.Remove(*outputFlag)

		data, err := os.ReadFile(*outputFlag)
		if err != nil {
			t.Fatalf("не удалось прочитать %s: %v", *outputFlag, err)
		}
		var out FileInfo
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("ошибка JSON: %v", err)
		}
		return out
	}

	// 🔹 Тест без dedupe
	out1 := runMerge(false)
	if out1.FullName != "RootA+RootB" {
		t.Errorf("ожидалось имя 'RootA+RootB', получено %q", out1.FullName)
	}

	// Проверяем объединённую структуру
	l2 := findDir(&out1, "level2")
	if l2 == nil {
		t.Fatalf("директория level2 не найдена")
	}
	shared := findDir(l2, "shared_dir")
	if shared == nil {
		t.Fatalf("директория shared_dir не найдена")
	}

	// При dedupe=false ожидаем 3 файла: общий и два уникальных
	if len(shared.Children) != 3 {
		t.Errorf("ожидалось 3 файла в shared_dir при dedupe=false, получено %d", len(shared.Children))
	}

	// 🔹 Тест с dedupe=true
	out2 := runMerge(true)
	shared2 := findDir(findDir(&out2, "level2"), "shared_dir")
	if shared2 == nil {
		t.Fatalf("директория shared_dir не найдена в dedupe=true")
	}
	// При dedupe=true общий файл один, а уникальные оба присутствуют
	if len(shared2.Children) != 2 {
		t.Errorf("ожидалось 2 файла в shared_dir при dedupe=true, получено %d", len(shared2.Children))
	}
}

func findDir(parent *FileInfo, name string) *FileInfo {
	for i := range parent.Children {
		if parent.Children[i].IsDir && parent.Children[i].FullName == name {
			return &parent.Children[i]
		}
	}
	return nil
}
