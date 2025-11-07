package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestMergeChildrenBasic(t *testing.T) {
	// 1️⃣ создаём 2 файла с простыми деревьями
	root1 := FileInfo{
		IsDir:      true,
		FullName:   "dirA",
		NameOnly:   "dirA",
		FullPath:   "dirA",
		ChildCount: 1,
		Children: []FileInfo{
			{IsDir: false, FullName: "a.txt", NameOnly: "a", SizeBytes: 10, SizeHuman: "10B", FullPath: "dirA/a.txt"},
		},
	}

	root2 := FileInfo{
		IsDir:      true,
		FullName:   "dirB",
		NameOnly:   "dirB",
		FullPath:   "dirB",
		ChildCount: 2,
		Children: []FileInfo{
			{IsDir: false, FullName: "b.txt", NameOnly: "b", SizeBytes: 20, SizeHuman: "20B", FullPath: "dirB/b.txt"},
			{IsDir: true, FullName: "sub", NameOnly: "sub", FullPath: "dirB/sub",
				Children:   []FileInfo{{IsDir: false, FullName: "x.txt", NameOnly: "x", SizeBytes: 5, FullPath: "dirB/sub/x.txt"}},
				ChildCount: 1,
			},
		},
	}

	f1, _ := os.CreateTemp("", "merge1_*.json")
	f2, _ := os.CreateTemp("", "merge2_*.json")
	defer os.Remove(f1.Name())
	defer os.Remove(f2.Name())

	json.NewEncoder(f1).Encode(root1)
	json.NewEncoder(f2).Encode(root2)
	f1.Close()
	f2.Close()

	// 2️⃣ подготавливаем флаги
	*mergeFlag = f1.Name() + "," + f2.Name()
	*mergeChildrenFlag = true
	*outputFlag = "test_merged.json"
	*prettyFlag = false
	*mergeFlatFlag = false
	*dedupeFlag = false

	// 3️⃣ выполняем merge
	mergeMode()
	defer os.Remove(*outputFlag)

	// 4️⃣ читаем результат и проверяем
	data, err := os.ReadFile(*outputFlag)
	if err != nil {
		t.Fatalf("не удалось прочитать результат: %v", err)
	}

	var out FileInfo
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("не удалось распарсить JSON: %v", err)
	}

	if !out.IsDir {
		t.Fatalf("ожидался корневой каталог, а не файл")
	}
	if out.FullName != "dirA+dirB" {
		t.Errorf("ожидалось имя 'dirA+dirB', получено %q", out.FullName)
	}
	if len(out.Children) != 3 {
		t.Errorf("ожидалось 3 элемента (a.txt, b.txt, sub), получено %d", len(out.Children))
	}
	foundSub := false
	for _, c := range out.Children {
		if c.FullName == "sub" {
			foundSub = true
			if len(c.Children) != 1 || c.Children[0].FullName != "x.txt" {
				t.Errorf("ошибка в объединённой поддиректории 'sub'")
			}
		}
	}
	if !foundSub {
		t.Errorf("не найдена объединённая поддиректория 'sub'")
	}
}
