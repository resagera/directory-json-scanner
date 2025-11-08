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

func MergeMode(cfg MergeConfig) {
	fmt.Printf("🔗 Объединение %d файлов...\n", len(cfg.Files))

	all := make([]model.FileInfo, 0, 10000)
	roots := make([]model.FileInfo, 0, len(cfg.Files))

	var seen map[string]struct{}
	if cfg.Dedupe {
		seen = make(map[string]struct{})
		fmt.Println("⚙️  Включено удаление дубликатов по FullPathOrig")
	} else {
		fmt.Println("⚙️  Дубликаты не будут удаляться")
	}

	for _, file := range cfg.Files {
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

		// []FileInfo
		if err := json.Unmarshal(data, &parsedFlat); err == nil && len(parsedFlat) > 0 {
			fmt.Printf("📄 %s: flat (%d)\n", file, len(parsedFlat))
			all = service.AppendFlatUnique(all, parsedFlat, seen)
			roots = append(roots, service.AssembleNestedFromFlat(parsedFlat))
			continue
		}
		// FileInfo
		if err := json.Unmarshal(data, &parsedTree); err == nil && (parsedTree.FullName != "" || len(parsedTree.Children) > 0) {
			fmt.Printf("🌲 %s: дерево (%d детей)\n", file, len(parsedTree.Children))
			all = service.AppendFlatUnique(all, service.FlattenTree(parsedTree), seen)
			roots = append(roots, parsedTree)
			continue
		}
		fmt.Printf("⚠️ %s: неизвестный формат\n", file)
	}

	if len(all) == 0 && len(roots) == 0 {
		fmt.Println("⚠️ Нет данных для объединения — проверьте входные JSON-файлы.")
		return
	}

	// Приоритет: --merge-children
	if cfg.MergeChildren {
		fmt.Println("🧩 Режим: объединение дочерних элементов корней (--merge-children)")
		root := service.MergeRootChildren(roots, cfg.Dedupe)
		service.ComputeDirSizes(&root)
		service.RecountChildCounts(&root)
		infrastructure.WriteFinalJSONAtomic(cfg.Output, root, cfg.Pretty)
		infrastructure.DiagnoseJSONShape(cfg.Output)
		fmt.Printf("✅ Итоговый корень: %s | %s\n", root.FullName, cfg.Output)
		return
	}

	// Обычная сборка
	if cfg.MergeFlat {
		fmt.Println("📤 Сохранение в формате flat ([]FileInfo)")
		infrastructure.WriteFlatJSONAtomic(cfg.Output, all, cfg.Pretty)
		infrastructure.DiagnoseJSONShape(cfg.Output)
		fmt.Printf("✅ Объединение завершено. Итоговый файл: %s\n", cfg.Output)
		return
	}

	fmt.Println("📤 Сборка иерархического дерева...")
	root := service.AssembleNestedFromFlat(all)
	service.ComputeDirSizes(&root)
	service.RecountChildCounts(&root)
	infrastructure.WriteFinalJSONAtomic(cfg.Output, root, cfg.Pretty)
	infrastructure.DiagnoseJSONShape(cfg.Output)
	fmt.Printf("✅ Объединение завершено. Итоговый файл: %s\n", cfg.Output)
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
