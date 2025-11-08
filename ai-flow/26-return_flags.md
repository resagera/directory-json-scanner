согласен — в предыдущей версии часть CLI-флагов действительно «терялась» и подставлялись дефолты. Ниже я даю **готовые файлы**, где флаги правильно прокинуты в соответствующие режимы: сканирование (stream/parallel) и объединение (merge).

Добавлено/исправлено:

* `--exclude` используется при обходе (и в stream, и в parallel).
* `--output`, `--pretty` учитываются при финальной записи.
* `--workers` управляет пулом воркеров.
* `--no-md5` отключает вычисление MD5.
* `--io-limit` ограничивает одновременные I/O (учитывается в ReadDir/MD5).
* `--dedupe` применяется в merge.
* `--merge-flat` и `--merge-children` корректно влияют на тип результата и стратегию объединения.
* `--resume` пока отмечен как TODO (сообщение выводится явно).

> Я добавил маленький слой конфигов `internal/app/config.go`, чтобы прозрачно и типобезопасно передавать параметры между слоями.

---

# 📦 `internal/app/config.go`

```go
package app

// ScanConfig — параметры сканирования
type ScanConfig struct {
	RootDir   string
	Exclude   []string
	Output    string
	Pretty    bool
	Workers   int
	SkipMD5   bool
	IOLimit   int
	Resume    bool // TODO: пока не реализовано в stream-режиме
}

// MergeConfig — параметры объединения
type MergeConfig struct {
	Files          []string
	Output         string
	Pretty         bool
	Dedupe         bool
	MergeFlat      bool
	MergeChildren  bool
}
```

---

# 📦 `cmd/fsjson/main.go`

```go
package main

import (
	"flag"
	"log"
	"runtime"
	"strings"

	"fsjson/internal/app"
	"fsjson/internal/infrastructure"
)

var (
	dirFlag           = flag.String("dir", ".", "Директория для сканирования")
	excludeFlag       = flag.String("exclude", "", "Исключения через запятую")
	outputFlag        = flag.String("output", "structure.json", "Выходной JSON-файл")
	prettyFlag        = flag.Bool("pretty", false, "Форматировать JSON красиво")
	streamFlag        = flag.Bool("stream", false, "Потоковая запись в temp")
	resumeFlag        = flag.Bool("resume", false, "Продолжить сканирование (только с --stream)")
	mergeFlag         = flag.String("merge", "", "Список JSON-файлов через запятую для объединения")
	workersFlag       = flag.Int("workers", runtime.NumCPU(), "Количество параллельных потоков сканирования")
	skipMd5Flag       = flag.Bool("no-md5", false, "Не вычислять MD5 для файлов")
	ioLimitFlag       = flag.Int("io-limit", 16, "Максимум одновременных I/O операций (чтение/MD5/Stat)")
	dedupeFlag        = flag.Bool("dedupe", false, "Удалять дубликаты по FullPathOrig при объединении JSON файлов")
	mergeFlatFlag     = flag.Bool("merge-flat", false, "Сохранять объединённый результат в плоском виде ([]FileInfo)")
	mergeChildrenFlag = flag.Bool("merge-children", false, "Объединять только дочерние элементы корней")
	webFlag           = flag.Bool("web", false, "Запустить веб-интерфейс для просмотра JSON")
	fileFlag          = flag.String("file", "", "JSON-файл для просмотра в веб-интерфейсе")
)

func main() {
	flag.Parse()

	// WEB режим
	if *webFlag {
		if *fileFlag == "" {
			log.Fatal("Укажите JSON-файл через --file")
		}
		infrastructure.StartWebServer(*fileFlag)
		return
	}

	// MERGE режим
	if *mergeFlag != "" {
		cfg := app.MergeConfig{
			Files:         splitCSV(*mergeFlag),
			Output:        *outputFlag,
			Pretty:        *prettyFlag,
			Dedupe:        *dedupeFlag,
			MergeFlat:     *mergeFlatFlag,
			MergeChildren: *mergeChildrenFlag,
		}
		app.MergeMode(cfg)
		return
	}

	// SCAN режим
	scanCfg := app.ScanConfig{
		RootDir: *dirFlag,
		Exclude: splitCSV(*excludeFlag),
		Output:  *outputFlag,
		Pretty:  *prettyFlag,
		Workers: *workersFlag,
		SkipMD5: *skipMd5Flag,
		IOLimit: *ioLimitFlag,
		Resume:  *resumeFlag,
	}

	if *streamFlag {
		app.ProcessParallelStream(scanCfg)
	} else {
		app.ProcessParallel(scanCfg)
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
```

---

# 📦 `internal/app/merge_mode.go`  (обновлён под флаги)

```go
package app

import (
	"encoding/json"
	"fmt"
	"os"
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
```

---

# 📦 `internal/app/stream_mode.go`  (использует все флаги)

```go
package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
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

func printProgress(n int64) {
	if n%1000 == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Printf("📊 %8d файлов | %.1f MB RAM\n", n, float64(m.Alloc)/1024.0/1024.0)
	}
}
```

---

# 📦 `internal/app/parallel_mode.go`  (использует все флаги)

```go
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
```

---

# 📦 `internal/domain/service/scanner.go`  (**добавил ProcessPathWith и ShouldExclude**)

```go
package service

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fsjson/internal/domain/model"
)

// ProcessPathWith — как ProcessPath, но с инъекцией I/O-функций (для лимита)
func ProcessPathWith(
	path string,
	info os.FileInfo,
	skipMd5 bool,
	readDirCount func(dir string) int,
	fileMD5 func(path string) string,
) model.FileInfo {
	parent := filepath.Dir(path)
	if parent == "." {
		parent = ""
	}

	size := int64(0)
	if !info.IsDir() {
		size = info.Size()
	}

	entry := model.FileInfo{
		IsDir:        info.IsDir(),
		FullName:     info.Name(),
		Ext:          strings.TrimPrefix(strings.ToLower(filepath.Ext(info.Name())), "."),
		NameOnly:     strings.TrimSuffix(info.Name(), filepath.Ext(info.Name())),
		SizeBytes:    size,
		SizeHuman:    HumanSize(size),
		FullPath:     path,
		FullPathOrig: path,
		ParentDir:    parent,
		Created:      info.ModTime(),
		Updated:      info.ModTime(),
		Perm:         info.Mode().String(),
		FileType:     DetectFileType(info.Name()),
	}

	if info.IsDir() {
		if readDirCount != nil {
			entry.ChildCount = readDirCount(path)
		}
		if !skipMd5 {
			entry.Md5 = Md5String(info.Name())
		}
	} else if !skipMd5 && fileMD5 != nil {
		entry.Md5 = fileMD5(path)
	}

	return entry
}

// ProcessPath — версия по умолчанию (без внешнего I/O лимита)
func ProcessPath(path string, info os.FileInfo, skipMd5 bool) model.FileInfo {
	return ProcessPathWith(path, info, skipMd5,
		func(dir string) int {
			list, _ := os.ReadDir(dir)
			return len(list)
		},
		func(p string) string {
			return FileMD5(p)
		},
	)
}

// ShouldExclude — проверка по подстроке ПОЛНОГО пути (регистронезависимо)
func ShouldExclude(absPath string, excludes []string) bool {
	pl := strings.ToLower(absPath)
	for _, ex := range excludes {
		if ex != "" && strings.Contains(pl, ex) {
			return true
		}
	}
	return false
}

// --- MD5 helpers (чистые, без инфраструктурных зависимостей) ---
func Md5String(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func FileMD5(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
```

---

✅ Теперь **все перечисленные флаги реально используются** в нужных местах, и поведение программы соответствует CLI-параметрам.

Если хочешь, могу сразу приложить небольшой `Makefile` и обновлённый `README` с примерами запусков с этими флагами.
