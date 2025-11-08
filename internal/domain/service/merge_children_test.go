package service

import (
	"testing"

	"fsjson/internal/domain/model"
)

func TestMergeChildrenBasic(t *testing.T) {
	dirA := model.FileInfo{
		IsDir:      true,
		FullName:   "dirA",
		NameOnly:   "dirA",
		FullPath:   "/root/dirA",
		ChildCount: 1,
		Children: []model.FileInfo{
			{FullName: "file1.txt", SizeBytes: 100, FileType: "text"},
		},
	}

	dirB := model.FileInfo{
		IsDir:      true,
		FullName:   "dirB",
		NameOnly:   "dirB",
		FullPath:   "/root/dirB",
		ChildCount: 1,
		Children: []model.FileInfo{
			{FullName: "file2.txt", SizeBytes: 200, FileType: "text"},
		},
	}

	root := MergeRootChildren([]model.FileInfo{dirA, dirB}, false)

	if root.FullName != "dirA+dirB" {
		t.Fatalf("ожидалось имя dirA+dirB, получено %s", root.FullName)
	}

	if root.ChildCount != 2 {
		t.Fatalf("ожидалось 2 дочерних элемента, получено %d", root.ChildCount)
	}

	if root.SizeBytes != 300 {
		t.Fatalf("ожидалось суммарный размер 300, получено %d", root.SizeBytes)
	}
}
