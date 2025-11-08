package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fsjson/internal/domain/model"
	"fsjson/internal/domain/service"
	"fsjson/internal/infrastructure"
)

// ProcessParallelStream выполняет параллельное сканирование с потоковой записью
func ProcessParallelStream(rootDir string) {
	start := time.Now()
	rootAbs, _ := filepath.Abs(rootDir)
	fmt.Printf("📁 Параллельное сканирование (stream): %s\n", rootAbs)

	tempFile := strings.ReplaceAll(filepath.Base(rootDir), string(os.PathSeparator), "_") + "_temp.json"
	f, err := os.OpenFile(tempFile, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	writer.WriteString("[\n")

	numWorkers := runtime.NumCPU()
	ioLimit := 16
	jobs := make(chan string, numWorkers*4)
	results := make(chan model.FileInfo, numWorkers*4)

	infrastructure.InitIOLimiter(ioLimit)

	var wg sync.WaitGroup
	var processed int64

	// 🔹 Воркеры
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				fi, err := os.Stat(path)
				if err != nil {
					continue
				}
				entry := service.ProcessPath(path, fi, false)
				if entry.FullName != "" {
					results <- entry
				}
			}
		}()
	}

	// 🔹 Writer
	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		first := true
		for r := range results {
			b, _ := json.Marshal(r)
			if !first {
				writer.WriteString(",\n")
			}
			writer.Write(b)
			first = false
			if atomic.AddInt64(&processed, 1)%500 == 0 {
				writer.Flush()
				printProgress(processed)
			}
		}
	}()

	// 🔹 Producer (WalkDir)
	go func() {
		defer close(jobs)
		filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
			if err == nil {
				jobs <- path
			}
			return nil
		})
	}()

	wg.Wait()
	close(results)
	writerWG.Wait()
	writer.WriteString("\n]\n")
	writer.Flush()

	fmt.Printf("✅ Потоковый JSON создан: %s\n", tempFile)

	flat, err := readFlatArrayFromFile(tempFile)
	if err != nil {
		log.Fatalf("Ошибка чтения temp: %v", err)
	}
	root := service.AssembleNestedFromFlat(flat)
	service.ComputeDirSizes(&root)
	infrastructure.WriteFinalJSONAtomic("result.json", root, true)
	fmt.Printf("🎉 Завершено. Файлов: %d | %v\n", processed, time.Since(start))
}

// Вспомогательная функция
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
