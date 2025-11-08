package app

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"fsjson/internal/domain/model"
	"fsjson/internal/domain/service"
	"fsjson/internal/infrastructure"
)

// MergeMode объединяет несколько JSON-файлов (flat или tree)
func MergeMode(mergeArg string) {
	files := strings.Split(mergeArg, ",")
	fmt.Printf("🔗 Объединение %d файлов...\n", len(files))

	all := make([]model.FileInfo, 0, 10000)
	roots := make([]model.FileInfo, 0, len(files))
	seen := make(map[string]struct{})

	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		fmt.Printf("📥 Чтение %s...\n", file)
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("❌ Ошибка чтения %s: %v\n", file, err)
			continue
		}

		var parsedFlat []model.FileInfo
		var parsedTree model.FileInfo

		if err := json.Unmarshal(data, &parsedFlat); err == nil && len(parsedFlat) > 0 {
			fmt.Printf("📄 %s: flat-массив (%d элементов)\n", file, len(parsedFlat))
			all = append(all, service.AppendFlatUnique(nil, parsedFlat, seen)...)
			roots = append(roots, service.AssembleNestedFromFlat(parsedFlat))
			continue
		}

		if err := json.Unmarshal(data, &parsedTree); err == nil &&
			(parsedTree.FullName != "" || len(parsedTree.Children) > 0) {
			fmt.Printf("🌲 %s: дерево (%d детей)\n", file, len(parsedTree.Children))
			all = append(all, service.FlattenTree(parsedTree)...)
			roots = append(roots, parsedTree)
			continue
		}

		fmt.Printf("⚠️ %s: не удалось определить формат\n", file)
	}

	if len(all) == 0 && len(roots) == 0 {
		fmt.Println("⚠️ Нет данных для объединения — проверьте входные JSON-файлы.")
		return
	}

	// === Собираем дерево ===
	fmt.Println("📤 Сборка иерархического дерева...")
	root := service.AssembleNestedFromFlat(all)
	service.ComputeDirSizes(&root)
	service.RecountChildCounts(&root)
	infrastructure.WriteFinalJSONAtomic("merged.json", root, true)
	infrastructure.DiagnoseJSONShape("merged.json")
	fmt.Printf("✅ Объединение завершено. Файл: merged.json (%d элементов)\n", len(all))
}

// --- вспомогательные функции сортировки ---
func sortRoots(roots []model.FileInfo) {
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].IsDir != roots[j].IsDir {
			return roots[i].IsDir
		}
		return strings.ToLower(roots[i].FullName) < strings.ToLower(roots[j].FullName)
	})
}
