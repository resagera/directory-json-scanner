package app

import (
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

// ProcessParallel — параллельное сканирование без потоковой записи
func ProcessParallel(cfg ScanConfig) {
	start := time.Now()
	rootAbs, _ := filepath.Abs(cfg.RootDir)
	fmt.Printf("📁 Начало сканирования: %s\n", rootAbs)
	fmt.Printf("⚙️  Workers: %d | I/O limit: %d | MD5: %v | pretty: %v\n",
		cfg.Workers, cfg.IOLimit, !cfg.SkipMD5, cfg.Pretty)

	infrastructure.InitIOLimiter(cfg.IOLimit)

	jobs := make(chan string, cfg.Workers*4)
	results := make(chan model.FileInfo, cfg.Workers*4)
	var wg sync.WaitGroup
	var processed int64

	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				if service.ShouldExclude(path, cfg.Exclude) {
					continue
				}
				fi, err := os.Stat(path)
				if err != nil {
					continue
				}
				entry := service.ProcessPathWith(path, fi, cfg.SkipMD5,
					func(dir string) int {
						return infrastructure.WithIOLimitValue(func() int {
							list, _ := os.ReadDir(dir)
							return len(list)
						})
					},
					func(p string) string {
						return infrastructure.WithIOLimitValue(func() string {
							return service.FileMD5(p)
						})
					},
				)
				if entry.FullName != "" {
					results <- entry
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		filepath.WalkDir(cfg.RootDir, func(path string, d os.DirEntry, err error) error {
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
	infrastructure.WriteFinalJSONAtomic(cfg.Output, root, cfg.Pretty)
	infrastructure.DiagnoseJSONShape(cfg.Output)

	fmt.Printf("✅ Готово. Файлов: %d | %v\n", processed, time.Since(start))
}

func printProgress(n int64) {
	if n%1000 == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Printf("📊 %8d файлов | %.1f MB RAM\n", n, float64(m.Alloc)/1024.0/1024.0)
	}
}
