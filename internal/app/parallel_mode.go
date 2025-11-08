package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"fsjson/internal/domain/model"
	"fsjson/internal/domain/service"
	"fsjson/internal/infrastructure"
)

// ProcessParallel выполняет обычное параллельное сканирование без потоковой записи
func ProcessParallel(rootDir string) {
	start := time.Now()
	rootAbs, _ := filepath.Abs(rootDir)
	fmt.Printf("📁 Начало сканирования: %s\n", rootAbs)

	numWorkers := runtime.NumCPU()
	jobs := make(chan string, numWorkers*4)
	results := make(chan model.FileInfo, numWorkers*4)
	var wg sync.WaitGroup
	var processed int64

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				fi, err := os.Stat(path)
				if err != nil {
					continue
				}
				results <- service.ProcessPath(path, fi, false)
			}
		}()
	}

	go func() {
		defer close(jobs)
		filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
			if err == nil {
				jobs <- path
			}
			return nil
		})
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var flat []model.FileInfo
	for r := range results {
		if r.FullName != "" {
			flat = append(flat, r)
			if atomic.AddInt64(&processed, 1)%1000 == 0 {
				printProgress(processed)
			}
		}
	}

	root := service.AssembleNestedFromFlat(flat)
	service.ComputeDirSizes(&root)
	infrastructure.WriteFinalJSONAtomic("result.json", root, true)

	fmt.Printf("✅ Готово. Файлов: %d | %v\n", processed, time.Since(start))
}

func printProgress(n int64) {
	if n%1000 == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Printf("📊 %8d файлов | %.1f MB RAM\n", n, float64(m.Alloc)/1024.0/1024.0)
	}
}

// Helper for debugging (optional JSON dump)
func debugWriteFlatJSON(arr []model.FileInfo) {
	data, _ := json.MarshalIndent(arr, "", "  ")
	os.WriteFile("debug_flat.json", data, 0644)
}
