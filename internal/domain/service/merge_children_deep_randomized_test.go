package service

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"fsjson/internal/domain/model"
)

// Генерирует и объединяет большие деревья, проверяет корректность и измеряет производительность
func TestMergeChildrenDeepRandomized(t *testing.T) {
	rand.Seed(time.Now().UnixNano())

	const (
		depth   = 5 // глубина вложенности
		breadth = 4 // ширина на каждом уровне
	)

	t.Logf("🧩 Генерация случайных деревьев (depth=%d, breadth=%d)...", depth, breadth)
	genStart := time.Now()
	treeA := genRandomTree("RootA", depth, breadth)
	treeB := genRandomTree("RootB", depth, breadth)
	genElapsed := time.Since(genStart)

	filesA := countFiles(&treeA)
	filesB := countFiles(&treeB)
	t.Logf("📁 Сгенерировано: A=%d файлов, B=%d файлов (%.2fs)", filesA, filesB, genElapsed.Seconds())

	// --- merge с dedupe=false ---
	t.Log("🚀 Объединение (dedupe=false)...")
	start := time.Now()
	merged := MergeRootChildren([]model.FileInfo{treeA, treeB}, false)
	mergeElapsed := time.Since(start)

	filesMerged := countFiles(&merged)
	dirsMerged := countDirs(&merged)
	t.Logf("✅ merge time: %v | files=%d | dirs=%d | rate=%.0f nodes/sec",
		mergeElapsed, filesMerged, dirsMerged,
		float64(filesA+filesB)/mergeElapsed.Seconds(),
	)

	// --- merge с dedupe=true ---
	t.Log("🚀 Объединение (dedupe=true)...")
	start2 := time.Now()
	mergedDedupe := MergeRootChildren([]model.FileInfo{treeA, treeB}, true)
	mergeElapsed2 := time.Since(start2)
	filesMerged2 := countFiles(&mergedDedupe)
	dirsMerged2 := countDirs(&mergedDedupe)

	t.Logf("✅ merge(dedupe=true) time: %v | files=%d | dirs=%d | rate=%.0f nodes/sec",
		mergeElapsed2, filesMerged2, dirsMerged2,
		float64(filesA+filesB)/mergeElapsed2.Seconds(),
	)

	if filesMerged2 > filesMerged {
		t.Fatalf("dedupe=true увеличил количество файлов (%d>%d)", filesMerged2, filesMerged)
	}

	checkTreeConsistency(t, &mergedDedupe)
}

// === helpers ===

// рекурсивный генератор дерева
func genRandomTree(name string, depth, breadth int) model.FileInfo {
	if depth == 0 {
		return file(name+".txt", int64(rand.Intn(500)+1))
	}
	children := []model.FileInfo{}
	for i := 0; i < breadth; i++ {
		if rand.Float64() < 0.35 {
			children = append(children, file(
				fmt.Sprintf("%s_file%d.txt", name, i),
				int64(rand.Intn(500)+100),
			))
		} else {
			children = append(children, genRandomTree(fmt.Sprintf("%s_sub%d", name, i), depth-1, breadth))
		}
	}
	return dir(name, children...)
}

// посчитать количество файлов
func countFiles(node *model.FileInfo) int {
	total := 0
	if !node.IsDir {
		return 1
	}
	for i := range node.Children {
		total += countFiles(&node.Children[i])
	}
	return total
}

// посчитать количество директорий
func countDirs(node *model.FileInfo) int {
	if !node.IsDir {
		return 0
	}
	total := 1
	for i := range node.Children {
		total += countDirs(&node.Children[i])
	}
	return total
}

// проверка консистентности
func checkTreeConsistency(t *testing.T, node *model.FileInfo) {
	if node.FullName == "" {
		t.Errorf("пустое имя узла обнаружено")
	}
	if node.IsDir {
		for i := range node.Children {
			checkTreeConsistency(t, &node.Children[i])
		}
	} else {
		if node.SizeBytes <= 0 {
			t.Errorf("файл %s имеет нулевой размер", node.FullPath)
		}
	}
}
