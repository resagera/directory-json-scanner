package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"fsjson/internal/domain/model"
	"fsjson/internal/domain/service"
	"fsjson/internal/infrastructure"
)

// ProcessParallelStream — параллельное сканирование с потоковой записью
func ProcessParallelStream(cfg ScanConfig) {
	start := time.Now()
	rootAbs, _ := filepath.Abs(cfg.RootDir)
	fmt.Printf("📁 Параллельное сканирование (stream): %s\n", rootAbs)
	fmt.Printf("⚙️  Workers: %d | I/O limit: %d | MD5: %v | pretty: %v\n",
		cfg.Workers, cfg.IOLimit, !cfg.SkipMD5, cfg.Pretty)
	if cfg.Resume {
		fmt.Println("ℹ️  --resume: TODO (пока не реализовано для stream-режима)")
	}

	tempFile := deriveTempName(cfg.Output)
	f, err := os.OpenFile(tempFile, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	_, _ = writer.WriteString("[\n")

	jobs := make(chan string, cfg.Workers*4)
	results := make(chan model.FileInfo, cfg.Workers*4)

	infrastructure.InitIOLimiter(cfg.IOLimit)

	var wg sync.WaitGroup
	var processed int64

	// Воркеры
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				// исключения
				if service.ShouldExclude(path, cfg.Exclude) {
					continue
				}
				fi, err := os.Stat(path)
				if err != nil {
					continue
				}
				// инъекция I/O-ограничений в ReadDir и FileMD5
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

	// Writer
	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		first := true
		encFirst := func() { first = false }
		for r := range results {
			b, _ := json.Marshal(r)
			if !first {
				_, _ = writer.WriteString(",\n")
			} else {
				encFirst()
			}
			_, _ = writer.Write(b)

			if atomic.AddInt64(&processed, 1)%500 == 0 {
				_ = writer.Flush()
				printProgress(processed)
			}
		}
	}()

	// Producer
	go func() {
		defer close(jobs)
		filepath.WalkDir(cfg.RootDir, func(path string, d os.DirEntry, err error) error {
			if err == nil {
				jobs <- path
			}
			return nil
		})
	}()

	wg.Wait()
	close(results)
	writerWG.Wait()
	_, _ = writer.WriteString("\n]\n")
	_ = writer.Flush()

	fmt.Printf("✅ Потоковый JSON создан: %s\n", tempFile)

	flat, err := readFlatArrayFromFile(tempFile)
	if err != nil {
		log.Fatalf("Ошибка чтения temp: %v", err)
	}
	root := service.AssembleNestedFromFlat(flat)
	service.ComputeDirSizes(&root)
	infrastructure.WriteFinalJSONAtomic(cfg.Output, root, cfg.Pretty)
	infrastructure.DiagnoseJSONShape(cfg.Output)

	fmt.Printf("🎉 Завершено. Файлов: %d | %v\n", processed, time.Since(start))
}

func deriveTempName(output string) string {
	if output == "" {
		return "scan_temp.json"
	}
	ext := filepath.Ext(output)
	base := output[:len(output)-len(ext)]
	return base + "_temp.json"
}

func readFlatArrayFromFile(path string) ([]model.FileInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var arr []model.FileInfo
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}
