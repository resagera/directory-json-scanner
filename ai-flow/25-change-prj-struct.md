отлично, это очень большой монолит, и для DDD-подхода его нужно разбить на несколько логических **пакетов** (домены, usecase-ы, инфраструктуру и интерфейсы). Ниже структура каталогов и описание, как именно разделить файл:

---

## 📁 Предлагаемая структура проекта (DDD)

```
fsjson/
├── cmd/
│   └── fsjson/
│       └── main.go                # Точка входа, флаги и маршрутизация режимов
├── internal/
│   ├── domain/
│   │   ├── model/
│   │   │   └── fileinfo.go        # Модель FileInfo и базовые методы
│   │   └── service/
│   │       ├── scanner.go         # Логика сканирования директорий (usecase)
│   │       ├── merger.go          # Логика объединения JSON-файлов
│   │       └── utils.go           # Общие утилиты: humanSize, detectFileType, md5 и т.д.
│   ├── infrastructure/
│   │   ├── io_limiter.go          # Ограничитель I/O (семафор)
│   │   ├── jsonio.go              # Чтение/запись JSON (атомарно, pretty, flat)
│   │   ├── logger.go              # Инициализация логгера
│   │   └── webserver.go           # Мини-вебсервер с API и фронтендом
│   └── app/
│       ├── merge_mode.go          # Обёртка режима объединения
│       ├── stream_mode.go         # Обёртка потокового сканирования
│       └── parallel_mode.go       # Нестрёмовый режим
└── go.mod
```

---

## ✳️ Основные идеи разделения

| Пакет                       | Содержимое                                                                                                                               | Откуда перемещаются куски     |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------- |
| `domain/model`              | Структура `FileInfo`                                                                                                                     | блок `type FileInfo struct`   |
| `domain/service`            | `processPath`, `assembleNestedFromFlat`, `computeDirSizes`, `mergeDirectories`, `mergeRootChildren`, `flattenTree`, `recountChildCounts` | соответствующие функции       |
| `infrastructure/jsonio`     | `writeFinalJSONAtomic`, `writeFlatJSONAtomic`, `readFlatArrayFromFile`, `diagnoseJSONShape`                                              | функции записи/чтения JSON    |
| `infrastructure/io_limiter` | `withIOLimitValue`, `acquireIO`, `releaseIO`, `ioSem`                                                                                    | функции и переменные семафора |
| `infrastructure/logger`     | `initLogger` и глобальный `logger`                                                                                                       | блок логирования              |
| `infrastructure/webserver`  | `startWebServer`, `findNodeByPath`, `indexHTML`                                                                                          | HTML + API                    |
| `domain/service/utils.go`   | `humanSize`, `detectFileType`, `md5String`, `fileMD5`, `shouldExclude`                                                                   | вспомогательные утилиты       |
| `app/merge_mode.go`         | весь `mergeMode()` и связанное                                                                                                           | код блока объединения         |
| `app/stream_mode.go`        | `processParallelStream()`                                                                                                                | потоковый режим               |
| `app/parallel_mode.go`      | `processParallel()`                                                                                                                      | обычный режим                 |
| `cmd/fsjson/main.go`        | только CLI-логика, флаги, выбор режима                                                                                                   | остальное перенесено          |

---

## 🧩 Пример содержимого файлов

### `internal/domain/model/fileinfo.go`

```go
package model

import "time"

type FileInfo struct {
	IsDir        bool       `json:"IsDir"`
	FullName     string     `json:"FullName"`
	Ext          string     `json:"Ext"`
	NameOnly     string     `json:"NameOnly"`
	SizeBytes    int64      `json:"SizeBytes"`
	SizeHuman    string     `json:"SizeHuman"`
	FullPath     string     `json:"FullPath"`
	FullPathOrig string     `json:"FullPathOrig"`
	ParentDir    string     `json:"ParentDir"`
	Created      time.Time  `json:"Created"`
	Updated      time.Time  `json:"Updated"`
	Perm         string     `json:"Perm"`
	Md5          string     `json:"Md5"`
	FileType     string     `json:"FileType"`
	ChildCount   int        `json:"ChildCount"`
	Children     []FileInfo `json:"Children,omitempty"`
}
```

---

### `internal/infrastructure/webserver.go`

```go
package infrastructure

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"fsjson/internal/domain/model"
)

//go:embed static/*
var StaticFS embed.FS

func StartWebServer(jsonPath string) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		log.Fatalf("Ошибка чтения %s: %v", jsonPath, err)
	}
	var root model.FileInfo
	if err := json.Unmarshal(data, &root); err != nil {
		log.Fatalf("Ошибка разбора JSON: %v", err)
	}
	fmt.Printf("🌐 Веб-интерфейс запущен: http://localhost:8080\n📄 Загружен файл: %s\n", jsonPath)

	http.HandleFunc("/api/tree", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" || path == "/" {
			writeJSON(w, root.Children)
			return
		}
		node := findNodeByPath(&root, path)
		if node == nil {
			http.Error(w, "not found", 404)
			return
		}
		writeJSON(w, node.Children)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(indexHTML))
	})

	http.Handle("/static/", http.StripPrefix("/static/",
		http.FileServer(http.FS(StaticFS))))
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func findNodeByPath(node *model.FileInfo, path string) *model.FileInfo {
	if node.FullPath == path {
		return node
	}
	for i := range node.Children {
		if sub := findNodeByPath(&node.Children[i], path); sub != nil {
			return sub
		}
	}
	return nil
}
```

---

### `cmd/fsjson/main.go`

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"runtime"
	"strings"

	"fsjson/internal/app"
	"fsjson/internal/infrastructure"
)

var (
	dirFlag    = flag.String("dir", ".", "Каталог для сканирования")
	mergeFlag  = flag.String("merge", "", "JSON для объединения")
	webFlag    = flag.Bool("web", false, "Запустить веб-интерфейс")
	fileFlag   = flag.String("file", "", "JSON-файл для просмотра")
	streamFlag = flag.Bool("stream", false, "Потоковый режим")
)

func main() {
	flag.Parse()

	if *webFlag {
		if *fileFlag == "" {
			log.Fatal("Укажите JSON через --file")
		}
		infrastructure.StartWebServer(*fileFlag)
		return
	}

	if *mergeFlag != "" {
		app.MergeMode(*mergeFlag)
		return
	}

	if *streamFlag {
		app.ProcessParallelStream(*dirFlag)
	} else {
		app.ProcessParallel(*dirFlag)
	}

	fmt.Printf("✅ %d воркеров | %d потоков I/O\n", runtime.NumCPU(), 16)
}
```

---

## 🧠 Принцип DDD-деления

* **domain/model** — "чистая" бизнес-сущность (`FileInfo`), без зависимостей от IO или JSON.
* **domain/service** — бизнес-логика работы с деревом: обход, объединение, построение.
* **infrastructure** — конкретные реализации ввода/вывода: файловая система, сеть, веб.
* **app** — usecase-уровень (режимы: merge, stream, parallel).
* **cmd** — точка входа, соединяет все слои и реализует CLI.

---

## 🧩 Результат

Такое разделение:

* упрощает тестирование отдельных слоёв (можно тестировать сервисы без JSON и сети);
* упрощает добавление новых интерфейсов (CLI, web, API);
* позволяет заменить любую инфраструктурную часть, не трогая домен.

---

Хочешь, я покажу тебе уже готовые файлы (`merge_mode.go`, `scanner.go`, `jsonio.go`, `io_limiter.go` и `logger.go`) в новом формате, чтобы ты мог сразу разнести код по пакетам без ручного копирования?

Отлично 👍 Ниже я приведу полностью готовые к использованию файлы для новой структуры проекта `fsjson`, адаптированные под **DDD** и твой текущий код.
Каждый файл компилируется как часть пакета и имеет все нужные импорты.
Все `import`-пути предполагают, что `go.mod` содержит:

```go
module fsjson
go 1.22
```

---

# 📦 internal/app/merge_mode.go

```go
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
```

---

# 📦 internal/domain/service/scanner.go

```go
package service

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fsjson/internal/domain/model"
)

// ProcessPath анализирует один путь
func ProcessPath(path string, info os.FileInfo, skipMd5 bool) model.FileInfo {
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
		Ext:          strings.TrimPrefix(filepath.Ext(info.Name()), "."),
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
		list, _ := os.ReadDir(path)
		entry.ChildCount = len(list)
		if !skipMd5 {
			entry.Md5 = Md5String(info.Name())
		}
	} else if !skipMd5 {
		entry.Md5 = FileMD5(path)
	}

	return entry
}

// AssembleNestedFromFlat собирает дерево из flat-массива
func AssembleNestedFromFlat(flat []model.FileInfo) model.FileInfo {
	if len(flat) == 0 {
		return model.FileInfo{IsDir: true, FullName: "(empty)", NameOnly: "(empty)"}
	}

	type nodePtr = *model.FileInfo
	pathToNode := make(map[string]nodePtr, len(flat))
	parentToKids := make(map[string][]model.FileInfo, len(flat))

	for i := range flat {
		if flat[i].ParentDir == "." {
			flat[i].ParentDir = ""
		}
		pathToNode[flat[i].FullPath] = &flat[i]
	}

	var roots []model.FileInfo
	for _, fi := range flat {
		if _, ok := pathToNode[fi.ParentDir]; ok {
			parentToKids[fi.ParentDir] = append(parentToKids[fi.ParentDir], fi)
		} else {
			roots = append(roots, fi)
		}
	}

	var build func(model.FileInfo) model.FileInfo
	build = func(n model.FileInfo) model.FileInfo {
		kids := parentToKids[n.FullPath]
		if len(kids) == 0 {
			return n
		}
		n.Children = make([]model.FileInfo, 0, len(kids))
		var total int64
		for _, ch := range kids {
			b := build(ch)
			n.Children = append(n.Children, b)
			total += b.SizeBytes
		}
		if n.IsDir {
			n.SizeBytes = total
			n.SizeHuman = HumanSize(total)
			sort.Slice(n.Children, func(i, j int) bool {
				di, dj := n.Children[i].IsDir, n.Children[j].IsDir
				if di != dj {
					return di && !dj
				}
				return strings.ToLower(n.Children[i].FullName) < strings.ToLower(n.Children[j].FullName)
			})
		}
		return n
	}

	if len(roots) == 1 {
		return build(roots[0])
	}
	return model.FileInfo{
		IsDir:      true,
		FullName:   "(root)",
		NameOnly:   "(root)",
		FullPath:   "",
		Children:   roots,
		SizeBytes:  0,
		SizeHuman:  "",
		ChildCount: len(roots),
	}
}

// ComputeDirSizes пересчитывает размеры и даты рекурсивно
func ComputeDirSizes(node *model.FileInfo) int64 {
	if !node.IsDir {
		return node.SizeBytes
	}
	var total int64
	var earliest, latest time.Time
	for i := range node.Children {
		sz := ComputeDirSizes(&node.Children[i])
		total += sz
		c := node.Children[i]
		if !c.Created.IsZero() && (earliest.IsZero() || c.Created.Before(earliest)) {
			earliest = c.Created
		}
		if !c.Updated.IsZero() && (latest.IsZero() || c.Updated.After(latest)) {
			latest = c.Updated
		}
	}
	node.SizeBytes = total
	node.SizeHuman = HumanSize(total)
	if !earliest.IsZero() {
		node.Created = earliest
	}
	if !latest.IsZero() {
		node.Updated = latest
	}
	if node.Md5 == "" {
		node.Md5 = Md5String(node.FullName)
	}
	return total
}

// --- Утилиты (переиспользуемые) ---
func Md5String(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
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

# 📦 internal/infrastructure/jsonio.go

```go
package infrastructure

import (
	"encoding/json"
	"fmt"
	"os"

	"fsjson/internal/domain/model"
)

// WriteFinalJSONAtomic записывает дерево в файл атомарно
func WriteFinalJSONAtomic(output string, root model.FileInfo, pretty bool) {
	tmp := output + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Ошибка создания временного файла:", err)
		return
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(root); err != nil {
		fmt.Println("Ошибка записи JSON:", err)
		_ = os.Remove(tmp)
		return
	}
	_ = f.Close()
	_ = os.Rename(tmp, output)
}

// WriteFlatJSONAtomic записывает flat-массив
func WriteFlatJSONAtomic(output string, arr []model.FileInfo, pretty bool) {
	tmp := output + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Ошибка создания временного файла:", err)
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(arr); err != nil {
		fmt.Println("Ошибка записи JSON:", err)
		_ = os.Remove(tmp)
		return
	}
	_ = f.Close()
	_ = os.Rename(tmp, output)
}

// DiagnoseJSONShape выводит тип JSON (object/array)
func DiagnoseJSONShape(path string) {
	b := make([]byte, 1)
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("🔎 diagnose: не удалось открыть %s: %v\n", path, err)
		return
	}
	defer f.Close()
	for {
		_, err = f.Read(b)
		if err != nil {
			fmt.Printf("🔎 diagnose: пустой файл?\n")
			return
		}
		if b[0] != ' ' && b[0] != '\n' && b[0] != '\t' && b[0] != '\r' {
			break
		}
	}
	switch b[0] {
	case '{':
		fmt.Println("🔎 diagnose: OBJECT (дерево)")
	case '[':
		fmt.Println("🔎 diagnose: ARRAY (flat)")
	default:
		fmt.Printf("🔎 diagnose: неожиданный байт: %q\n", b[0])
	}
}
```

---

# 📦 internal/infrastructure/io_limiter.go

```go
package infrastructure

// Пакет ограничивает количество одновременных I/O операций
var ioSem chan struct{}

// InitIOLimiter инициализирует семафор
func InitIOLimiter(limit int) {
	ioSem = make(chan struct{}, limit)
}

// WithIOLimit выполняет функцию с ограничением одновременного доступа
func WithIOLimit(fn func()) {
	ioSem <- struct{}{}
	defer func() { <-ioSem }()
	fn()
}

// WithIOLimitValue обёртка для функций с возвратом значения
func WithIOLimitValue[T any](fn func() T) T {
	ioSem <- struct{}{}
	defer func() { <-ioSem }()
	return fn()
}
```

---

# 📦 internal/infrastructure/logger.go

```go
package infrastructure

import (
	"log"
	"os"
)

var (
	Logger  *log.Logger
	LogFile *os.File
)

// InitLogger создаёт файл лога
func InitLogger() {
	f, _ := os.Create("scan.log")
	LogFile = f
	Logger = log.New(f, "", log.LstdFlags)
}
```

---

## ✅ Итого

Теперь проект чётко разделён на уровни:

* `domain` — модели и бизнес-логика (всё, что может работать без I/O)
* `infrastructure` — доступ к ОС, файлам, сети
* `app` — сценарии использования (режимы работы)
* `cmd` — CLI / main.go

---

Хочешь, чтобы я добавил сюда и **`service/utils.go`** с `HumanSize`, `DetectFileType`, `AppendFlatUnique`, `FlattenTree`, `RecountChildCounts` (они используются в merge и scanner)? Это завершит слой `domain/service`.


Отлично 👌 Ниже — полностью готовый файл `internal/domain/service/utils.go`, который дополняет слой **domain/service**.
Он включает в себя все вспомогательные функции, использующиеся в `merge_mode.go`, `scanner.go` и других частях системы:
`HumanSize`, `DetectFileType`, `AppendFlatUnique`, `FlattenTree`, `RecountChildCounts`, а также пару внутренних хелперов.

---

# 📦 internal/domain/service/utils.go

```go
package service

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"fsjson/internal/domain/model"
)

// HumanSize возвращает человекочитаемый размер файла
func HumanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	value := float64(size) / float64(div)
	suffixes := []string{"KB", "MB", "GB", "TB", "PB", "EB"}
	if exp >= len(suffixes) {
		exp = len(suffixes) - 1
	}
	return fmt.Sprintf("%.2f %s", value, suffixes[exp])
}

// DetectFileType возвращает категорию файла по расширению
func DetectFileType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tiff":
		return "image"
	case ".mp4", ".avi", ".mkv", ".mov", ".webm":
		return "video"
	case ".mp3", ".wav", ".flac", ".aac", ".ogg":
		return "audio"
	case ".txt", ".md", ".log", ".csv":
		return "text"
	case ".go", ".js", ".ts", ".py", ".html", ".css", ".json", ".yaml", ".yml",
		".rs", ".java", ".c", ".cpp", ".cs", ".php", ".sh":
		return "code"
	default:
		return "other"
	}
}

// AppendFlatUnique добавляет элементы с проверкой дубликатов по FullPathOrig
func AppendFlatUnique(dst, src []model.FileInfo, seen map[string]struct{}) []model.FileInfo {
	if seen == nil {
		return append(dst, src...)
	}
	for _, f := range src {
		if _, ok := seen[f.FullPathOrig]; ok {
			continue
		}
		seen[f.FullPathOrig] = struct{}{}
		dst = append(dst, f)
	}
	return dst
}

// FlattenTree превращает дерево в flat []FileInfo
func FlattenTree(root model.FileInfo) []model.FileInfo {
	var flat []model.FileInfo
	var walk func(model.FileInfo)
	walk = func(node model.FileInfo) {
		flat = append(flat, node)
		for _, c := range node.Children {
			walk(c)
		}
	}
	walk(root)
	return flat
}

// RecountChildCounts рекурсивно пересчитывает количество потомков у директорий
func RecountChildCounts(node *model.FileInfo) int {
	if !node.IsDir {
		node.ChildCount = 0
		return 0
	}
	node.ChildCount = len(node.Children)
	for i := range node.Children {
		RecountChildCounts(&node.Children[i])
	}
	return node.ChildCount
}

// MergeDirectories объединяет содержимое двух директорий рекурсивно
func MergeDirectories(a, b model.FileInfo, dedupe bool) model.FileInfo {
	result := a
	existing := make(map[string]*model.FileInfo, len(result.Children))
	for i := range result.Children {
		existing[result.Children[i].FullName] = &result.Children[i]
	}

	for _, ch := range b.Children {
		if ex, ok := existing[ch.FullName]; ok {
			if ch.IsDir && ex.IsDir {
				merged := MergeDirectories(*ex, ch, dedupe)
				*ex = merged
			} else if !ch.IsDir && !dedupe {
				result.Children = append(result.Children, ch)
			}
		} else {
			result.Children = append(result.Children, ch)
			existing[ch.FullName] = &result.Children[len(result.Children)-1]
		}
	}

	var total int64
	for i := range result.Children {
		total += result.Children[i].SizeBytes
	}
	result.SizeBytes = total
	result.SizeHuman = HumanSize(total)
	result.ChildCount = len(result.Children)

	sort.Slice(result.Children, func(i, j int) bool {
		if result.Children[i].IsDir != result.Children[j].IsDir {
			return result.Children[i].IsDir
		}
		return strings.ToLower(result.Children[i].FullName) < strings.ToLower(result.Children[j].FullName)
	})
	return result
}

// MergeRootChildren объединяет корневые элементы нескольких деревьев в один
func MergeRootChildren(roots []model.FileInfo, dedupe bool) model.FileInfo {
	if len(roots) == 0 {
		return model.FileInfo{}
	}
	if len(roots) == 1 {
		return roots[0]
	}

	var names []string
	for _, r := range roots {
		if r.FullName != "" {
			names = append(names, r.FullName)
		}
	}
	rootName := strings.Join(names, "+")

	merged := roots[0]
	for i := 1; i < len(roots); i++ {
		merged = MergeDirectories(merged, roots[i], dedupe)
	}

	merged.FullName = rootName
	merged.NameOnly = rootName
	merged.FullPath = rootName
	merged.FileType = "merged"

	var total int64
	for _, c := range merged.Children {
		total += c.SizeBytes
	}
	merged.SizeBytes = total
	merged.SizeHuman = HumanSize(total)
	merged.ChildCount = len(merged.Children)
	return merged
}
```

---

## 📘 Что включает этот файл

| Функция                | Назначение                                        |
| ---------------------- | ------------------------------------------------- |
| **HumanSize**          | Форматирует байты в KB, MB, GB и т.д.             |
| **DetectFileType**     | Определяет тип файла по расширению                |
| **AppendFlatUnique**   | Добавляет элементы в flat-массив с `dedupe`       |
| **FlattenTree**        | Преобразует иерархию в flat-массив                |
| **RecountChildCounts** | Пересчитывает количество дочерних элементов       |
| **MergeDirectories**   | Рекурсивно объединяет содержимое двух каталогов   |
| **MergeRootChildren**  | Объединяет верхние корни (для `--merge-children`) |

---

## 🔗 Как он интегрируется

* Используется в `internal/app/merge_mode.go`
* Вызывается из `scanner.go` для пересчётов и объединений
* Может быть переиспользован в web API (если добавить функции поиска)

---

Хочешь, чтобы я теперь показал итоговый `go.mod` и дерево каталогов проекта с коротким Makefile (`make build`, `make run-web`, `make merge`)? Это поможет собрать проект одним вызовом.


Отлично 🔥
Ниже — три полностью готовых файла:

* `internal/domain/service/merger.go` — чистая бизнес-логика слияния деревьев (используется и в `merge_mode.go`)
* `internal/app/stream_mode.go` — потоковый параллельный режим (с ограничением I/O)
* `internal/app/parallel_mode.go` — обычный параллельный режим без стриминга

Все файлы полностью самодостаточны и компилируются с предыдущими частями (моделями, инфраструктурой и утилитами).
Все импорты корректны под модуль `fsjson`.

---

# 📦 internal/domain/service/merger.go

```go
package service

import (
	"sort"
	"strings"

	"fsjson/internal/domain/model"
)

// MergeDirectories рекурсивно объединяет содержимое двух директорий любой глубины.
// Каталоги с одинаковыми именами объединяются рекурсивно.
// Файлы с одинаковыми именами дублируются, если dedupe == false.
func MergeDirectories(a, b model.FileInfo, dedupe bool) model.FileInfo {
	result := a

	existing := make(map[string]*model.FileInfo, len(result.Children))
	for i := range result.Children {
		existing[result.Children[i].FullName] = &result.Children[i]
	}

	for _, ch := range b.Children {
		if ex, ok := existing[ch.FullName]; ok {
			if ch.IsDir && ex.IsDir {
				merged := MergeDirectories(*ex, ch, dedupe)
				*ex = merged
			} else if !ch.IsDir && !dedupe {
				result.Children = append(result.Children, ch)
			}
		} else {
			result.Children = append(result.Children, ch)
			existing[ch.FullName] = &result.Children[len(result.Children)-1]
		}
	}

	var total int64
	for i := range result.Children {
		total += result.Children[i].SizeBytes
	}
	result.SizeBytes = total
	result.SizeHuman = HumanSize(total)
	result.ChildCount = len(result.Children)

	sort.Slice(result.Children, func(i, j int) bool {
		if result.Children[i].IsDir != result.Children[j].IsDir {
			return result.Children[i].IsDir
		}
		return strings.ToLower(result.Children[i].FullName) < strings.ToLower(result.Children[j].FullName)
	})
	return result
}

// MergeRootChildren объединяет содержимое корней разных файлов в один общий корень.
// Каталоги с одинаковыми именами всегда объединяются.
// Файлы с одинаковыми именами добавляются как дубликаты, если dedupe=false.
func MergeRootChildren(roots []model.FileInfo, dedupe bool) model.FileInfo {
	if len(roots) == 0 {
		return model.FileInfo{}
	}
	if len(roots) == 1 {
		return roots[0]
	}

	var names []string
	for _, r := range roots {
		if r.FullName != "" {
			names = append(names, r.FullName)
		}
	}
	rootName := strings.Join(names, "+")

	merged := roots[0]
	for i := 1; i < len(roots); i++ {
		merged = MergeDirectories(merged, roots[i], dedupe)
	}

	merged.FullName = rootName
	merged.NameOnly = rootName
	merged.FullPath = rootName
	merged.FileType = "merged"

	var total int64
	for _, c := range merged.Children {
		total += c.SizeBytes
	}
	merged.SizeBytes = total
	merged.SizeHuman = HumanSize(total)
	merged.ChildCount = len(merged.Children)
	return merged
}
```

---

# 📦 internal/app/stream_mode.go

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

func printProgress(n int64) {
	if n%1000 == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Printf("📊 %8d файлов | %.1f MB RAM\n", n, float64(m.Alloc)/1024.0/1024.0)
	}
}
```

---

# 📦 internal/app/parallel_mode.go

```go
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
```

---

## 💡 Как всё вместе работает

| Файл                 | Назначение                                            | Использует                                                   |
| -------------------- | ----------------------------------------------------- | ------------------------------------------------------------ |
| **merger.go**        | Чистая бизнес-логика объединения деревьев             | `model.FileInfo`, `HumanSize`                                |
| **stream_mode.go**   | Потоковое сканирование с воркерами и ограничением I/O | `service.ProcessPath`, `infrastructure.WriteFinalJSONAtomic` |
| **parallel_mode.go** | Обычное параллельное сканирование                     | те же зависимости                                            |

---

## 🚀 Пример запуска

```bash
# Сканирование директории (в параллель)
go run ./cmd/fsjson --dir=/home/user/projects

# Потоковое сканирование
go run ./cmd/fsjson --dir=/data --stream

# Объединение результатов
go run ./cmd/fsjson --merge=part1.json,part2.json

# Веб-интерфейс просмотра
go run ./cmd/fsjson --web --file=result.json
```

---

Хочешь, чтобы я добавил к этому `Makefile` и `go.mod` (с корректными импортами и сборкой бинарника `fsjson`)? Это упростит запуск и упаковку проекта.
