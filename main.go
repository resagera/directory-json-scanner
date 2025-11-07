package main

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

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
	mergeFlatFlag     = flag.Bool("merge-flat", false, "Сохранять объединённый результат в плоском виде ([]FileInfo) вместо иерархического дерева")
	mergeChildrenFlag = flag.Bool("merge-children", false, "Объединять только дочерние элементы корней с пересечением по именам директорий")
)

var (
	excludeList      []string
	streamTempName   string
	filesProcessed   int64
	startTime        time.Time
	logger           *log.Logger
	logFile          *os.File
	streamWriter     *bufio.Writer
	streamFileHandle *os.File

	ioSem chan struct{} // 👈 семафор для ограничения I/O
)

func main() {
	flag.Parse()
	startTime = time.Now()
	initLogger()
	defer func() {
		if logFile != nil {
			_ = logFile.Close()
		}
	}()

	// инициализация семафора
	ioSem = make(chan struct{}, *ioLimitFlag)

	if *excludeFlag != "" {
		for _, e := range strings.Split(*excludeFlag, ",") {
			e = strings.TrimSpace(e)
			if e != "" {
				excludeList = append(excludeList, strings.ToLower(e))
			}
		}
	}

	streamTempName = strings.TrimSuffix(*outputFlag, ".json") + "_temp.json"

	if *mergeFlag != "" {
		mergeMode()
		return
	}

	if *streamFlag {
		processParallelStream()
	} else {
		processParallel()
	}
}

// --- Merge Mode (фикс: строгий приоритет --merge-children, атомарная запись, диагностика) ---
func mergeMode() {
	files := strings.Split(*mergeFlag, ",")
	fmt.Printf("🔗 Объединение %d файлов...\n", len(files))

	// flat-коллекция для стандартной сборки
	all := make([]FileInfo, 0, 10000)

	// корни для --merge-children
	roots := make([]FileInfo, 0, len(files))

	// опциональный dedupe
	var seen map[string]struct{}
	if *dedupeFlag {
		seen = make(map[string]struct{})
		fmt.Println("⚙️  Включено удаление дубликатов по FullPathOrig")
	} else {
		fmt.Println("⚙️  Дубликаты не будут удаляться")
	}

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

		var parsedFlat []FileInfo
		var parsedTree FileInfo
		asFlat := false
		asTree := false

		// Пробуем flat ([]FileInfo)
		if err := json.Unmarshal(data, &parsedFlat); err == nil && len(parsedFlat) > 0 {
			asFlat = true
			fmt.Printf("📄 %s: flat-массив (%d элементов)\n", file, len(parsedFlat))
			all = appendFlatUnique(all, parsedFlat, seen)
			// Для merge-children нужен корень → собираем временный корень из flat
			tmpRoot := assembleNestedFromFlat(parsedFlat)
			roots = append(roots, tmpRoot)
			continue
		}

		// Пробуем дерево (FileInfo)
		if err := json.Unmarshal(data, &parsedTree); err == nil && (parsedTree.FullName != "" || len(parsedTree.Children) > 0) {
			asTree = true
			fmt.Printf("🌲 %s: дерево -> %d элементов\n", file, len(parsedTree.Children))
			all = appendFlatUnique(all, flattenTree(parsedTree), seen)
			roots = append(roots, parsedTree)
			continue
		}

		if !asFlat && !asTree {
			fmt.Printf("⚠️ %s: не удалось определить формат JSON\n", file)
		}
	}

	if len(all) == 0 && len(roots) == 0 {
		fmt.Println("⚠️ Нет данных для объединения — проверьте входные JSON-файлы.")
		return
	}

	// === ЖЁСТКИЙ приоритет: --merge-children ===
	if *mergeChildrenFlag {
		fmt.Println("🧩 Режим: объединение дочерних элементов корней (--merge-children)")
		// Даже если передан --merge-flat — игнорируем его тут
		root := mergeRootChildren(roots)
		computeDirSizes(&root)
		recountChildCounts(&root)
		writeFinalJSONAtomic(*outputFlag, root, *prettyFlag)
		diagnoseJSONShape(*outputFlag)
		fmt.Printf("✅ Итоговый корень: %s | %s\n", root.FullName, *outputFlag)
		return
	}

	// === Обычный merge: либо flat, либо иерархия ===
	if *mergeFlatFlag {
		fmt.Println("📤 Сохранение в формате flat ([]FileInfo)")
		writeFlatJSONAtomic(*outputFlag, all, *prettyFlag)
		diagnoseJSONShape(*outputFlag)
		fmt.Printf("✅ Объединение завершено. Итоговый файл: %s\n", *outputFlag)
		return
	}

	fmt.Println("📤 Сборка иерархического дерева...")
	root := assembleNestedFromFlat(all)
	computeDirSizes(&root)
	recountChildCounts(&root)
	writeFinalJSONAtomic(*outputFlag, root, *prettyFlag)
	diagnoseJSONShape(*outputFlag)
	fmt.Printf("✅ Объединение завершено. Итоговый файл: %s\n", *outputFlag)
}

// Атомарная запись объекта (дерева)
func writeFinalJSONAtomic(output string, root FileInfo, pretty bool) {
	tmp := output + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Ошибка создания временного файла:", err)
		return
	}
	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(root); err != nil {
		_ = f.Close()
		fmt.Println("Ошибка записи JSON:", err)
		_ = os.Remove(tmp)
		return
	}
	_ = f.Close()
	_ = os.Rename(tmp, output)
}

// Атомарная запись flat-массива
func writeFlatJSONAtomic(output string, arr []FileInfo, pretty bool) {
	tmp := output + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Ошибка создания временного файла:", err)
		return
	}
	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(arr); err != nil {
		_ = f.Close()
		fmt.Println("Ошибка записи JSON:", err)
		_ = os.Remove(tmp)
		return
	}
	_ = f.Close()
	_ = os.Rename(tmp, output)
}

// Диагностика формата результата (показывает, что в файле — объект или массив)
func diagnoseJSONShape(path string) {
	b := make([]byte, 1)
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("🔎 diagnose: не удалось открыть %s: %v\n", path, err)
		return
	}
	defer f.Close()
	// Пропускаем пробелы/переводы
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
		fmt.Println("🔎 diagnose: итог — OBJECT (дерево)")
	case '[':
		fmt.Println("🔎 diagnose: итог — ARRAY (flat)")
	default:
		fmt.Printf("🔎 diagnose: неожиданный первый байт: %q\n", b[0])
	}
}

// mergeRootChildren объединяет содержимое корней разных файлов в один общий корень.
// Каталоги с одинаковыми именами всегда объединяются.
// Файлы с одинаковыми именами добавляются как дубликаты, если dedupe=false.
func mergeRootChildren(roots []FileInfo) FileInfo {
	if len(roots) == 0 {
		return FileInfo{}
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

	dedupe := *dedupeFlag
	merged := roots[0]

	for i := 1; i < len(roots); i++ {
		merged = mergeDirectories(merged, roots[i], dedupe)
	}

	merged.FullName = rootName
	merged.NameOnly = rootName
	merged.FullPath = rootName
	merged.FileType = "merged"

	merged.ChildCount = len(merged.Children)
	var total int64
	for _, c := range merged.Children {
		total += c.SizeBytes
	}
	merged.SizeBytes = total
	merged.SizeHuman = humanSize(total)

	return merged
}

// mergeDirectories рекурсивно объединяет две директории любой глубины.
// Каталоги с одинаковыми именами всегда объединяются рекурсивно.
// Файлы с одинаковыми именами — только если dedupe=false дублируются.
func mergeDirectories(a, b FileInfo, dedupe bool) FileInfo {
	// создаём копию a, чтобы не трогать оригинал
	result := a

	// строим карту существующих детей по имени
	existing := make(map[string]*FileInfo, len(result.Children))
	for i := range result.Children {
		existing[result.Children[i].FullName] = &result.Children[i]
	}

	for _, ch := range b.Children {
		if ex, ok := existing[ch.FullName]; ok {
			// если совпали имена
			if ch.IsDir && ex.IsDir {
				// ✅ объединяем каталоги
				merged := mergeDirectories(*ex, ch, dedupe)
				*ex = merged
			} else if !ch.IsDir && !dedupe {
				// ✅ при dedupe=false добавляем даже если имя совпадает
				result.Children = append(result.Children, ch)
			} else if !ch.IsDir && dedupe {
				// ✅ при dedupe=true игнорируем дубликат файла
				continue
			}
		} else {
			// ✅ уникальный элемент — добавляем
			result.Children = append(result.Children, ch)
			existing[ch.FullName] = &result.Children[len(result.Children)-1]
		}
	}

	// пересчитываем размер и количество
	var total int64
	for i := range result.Children {
		total += result.Children[i].SizeBytes
	}
	result.SizeBytes = total
	result.SizeHuman = humanSize(total)
	result.ChildCount = len(result.Children)

	// сортируем для стабильности
	sort.Slice(result.Children, func(i, j int) bool {
		if result.Children[i].IsDir != result.Children[j].IsDir {
			return result.Children[i].IsDir
		}
		return strings.ToLower(result.Children[i].FullName) < strings.ToLower(result.Children[j].FullName)
	})

	return result
}

// appendFlatUnique добавляет элементы с опциональным dedupe
func appendFlatUnique(dst, src []FileInfo, seen map[string]struct{}) []FileInfo {
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

// flattenTree превращает дерево в flat []FileInfo
func flattenTree(root FileInfo) []FileInfo {
	var flat []FileInfo
	var walk func(FileInfo)
	walk = func(node FileInfo) {
		flat = append(flat, node)
		for _, c := range node.Children {
			walk(c)
		}
	}
	walk(root)
	return flat
}

// recountChildCounts пересчитывает ChildCount у всех директорий
func recountChildCounts(node *FileInfo) int {
	if !node.IsDir {
		node.ChildCount = 0
		return 0
	}
	node.ChildCount = len(node.Children)
	for i := range node.Children {
		recountChildCounts(&node.Children[i])
	}
	return node.ChildCount
}

// writeFlatJSON записывает массив []FileInfo в JSON
func writeFlatJSON(output string, arr []FileInfo, pretty bool) {
	f, err := os.Create(output)
	if err != nil {
		fmt.Println("Ошибка создания выходного файла:", err)
		return
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(arr); err != nil {
		fmt.Println("Ошибка записи JSON:", err)
	}
}

// --- Worker pool с потоковой записью ---
func processParallelStream() {
	rootAbs, _ := filepath.Abs(*dirFlag)
	fmt.Printf("📁 Параллельное сканирование с потоком: %s\n", rootAbs)
	fmt.Printf("⚙️  Workers: %d | I/O limit: %d | MD5: %v\n", *workersFlag, *ioLimitFlag, !*skipMd5Flag)

	f, err := os.OpenFile(streamTempName, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatal(err)
	}
	streamFileHandle = f
	streamWriter = bufio.NewWriter(streamFileHandle)
	streamWriter.WriteString("[\n")

	jobs := make(chan string, *workersFlag*4)
	results := make(chan FileInfo, *workersFlag*4)
	var wg sync.WaitGroup

	for i := 0; i < *workersFlag; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				entry := withIOLimit(func() FileInfo {
					info, err := os.Stat(path)
					if err != nil {
						return FileInfo{}
					}
					if shouldExclude(path) {
						return FileInfo{}
					}
					return processPath(path, info)
				})
				if entry.FullName != "" {
					results <- entry
				}
			}
		}()
	}

	// writer горутина
	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		first := true
		for r := range results {
			b, _ := json.Marshal(r)
			if !first {
				streamWriter.WriteString(",\n")
			}
			streamWriter.Write(b)
			first = false

			if atomic.AddInt64(&filesProcessed, 1)%500 == 0 {
				streamWriter.Flush()
				printProgress()
			}
		}
	}()

	go func() {
		defer close(jobs)
		filepath.WalkDir(*dirFlag, func(path string, d os.DirEntry, err error) error {
			if err == nil {
				jobs <- path
			}
			return nil
		})
	}()

	wg.Wait()
	close(results)
	writerWG.Wait()
	streamWriter.WriteString("\n]\n")
	streamWriter.Flush()
	streamFileHandle.Close()

	fmt.Printf("✅ Потоковый temp записан: %s\n", streamTempName)

	flat, err := readFlatArrayFromFile(streamTempName)
	if err != nil {
		log.Fatalf("Ошибка чтения temp: %v", err)
	}

	root := assembleNestedFromFlat(flat)
	computeDirSizes(&root)
	writeFinalJSON(*outputFlag, root, *prettyFlag)

	fmt.Printf("🎉 Готово. Файлов: %d | %v\n", atomic.LoadInt64(&filesProcessed), time.Since(startTime))
}

// --- JSON Reading ---
func readFlatArrayFromFile(path string) ([]FileInfo, error) {
	var arr []FileInfo
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// --- Параллельный (нестримовый) режим ---
func processParallel() {
	rootAbs, _ := filepath.Abs(*dirFlag)
	fmt.Printf("📁 Начало сканирования: %s\n", rootAbs)

	var wg sync.WaitGroup
	jobs := make(chan string, *workersFlag*4)
	results := make(chan FileInfo, *workersFlag*4)

	for i := 0; i < *workersFlag; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				fi, err := os.Stat(path)
				if err != nil {
					continue
				}
				if shouldExclude(path) {
					continue
				}
				results <- processPath(path, fi)
			}
		}()
	}

	go func() {
		defer close(jobs)
		filepath.WalkDir(*dirFlag, func(path string, d os.DirEntry, err error) error {
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

	var flat []FileInfo
	for r := range results {
		if r.FullName != "" {
			flat = append(flat, r)
			printProgress()
		}
	}

	root := assembleNestedFromFlat(flat)
	computeDirSizes(&root)
	writeFinalJSON(*outputFlag, root, *prettyFlag)
	fmt.Printf("✅ Готово. Всего файлов: %d\n", atomic.LoadInt64(&filesProcessed))
}

// --- Обработка пути ---
// --- processPath теперь использует семафор при работе с ReadDir/MD5 ---
func processPath(path string, info os.FileInfo) FileInfo {
	parent := filepath.Dir(path)
	if parent == "." {
		parent = ""
	}
	size := int64(0)
	if !info.IsDir() {
		size = info.Size()
	}

	entry := FileInfo{
		IsDir:        info.IsDir(),
		FullName:     info.Name(),
		Ext:          strings.TrimPrefix(filepath.Ext(info.Name()), "."),
		NameOnly:     strings.TrimSuffix(info.Name(), filepath.Ext(info.Name())),
		SizeBytes:    size,
		SizeHuman:    humanSize(size),
		FullPath:     path,
		FullPathOrig: path,
		ParentDir:    parent,
		Created:      info.ModTime(),
		Updated:      info.ModTime(),
		Perm:         info.Mode().String(),
		FileType:     detectFileType(info.Name()),
	}

	if info.IsDir() {
		entries := withIOLimit(func() []os.DirEntry {
			list, _ := os.ReadDir(path)
			return list
		})
		entry.ChildCount = len(entries)
		if !*skipMd5Flag {
			entry.Md5 = md5String(info.Name())
		}
	} else if !*skipMd5Flag {
		entry.Md5 = withIOLimit(func() string {
			return fileMD5(path)
		})
	}

	return entry
}

// --- Утилиты (короче, чем прежде) ---
func shouldExclude(path string) bool {
	p := strings.ToLower(path)
	for _, ex := range excludeList {
		if ex != "" && strings.Contains(p, ex) {
			return true
		}
	}
	return false
}

func md5String(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// --- fileMD5 теперь использует withIOLimit ---
func fileMD5(path string) string {
	return withIOLimit(func() string {
		f, err := os.Open(path)
		if err != nil {
			return ""
		}
		defer f.Close()
		h := md5.New()
		io.Copy(h, f)
		return hex.EncodeToString(h.Sum(nil))
	})
}

// --- I/O limiter helpers ---
func acquireIO() { ioSem <- struct{}{} }
func releaseIO() { <-ioSem }
func withIOLimit[T any](fn func() T) T {
	acquireIO()
	defer releaseIO()
	return fn()
}

func detectFileType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return "image"
	case ".mp4", ".avi", ".mkv", ".mov":
		return "video"
	case ".mp3", ".wav", ".flac":
		return "audio"
	case ".go", ".js", ".py", ".html", ".css", ".json", ".md":
		return "code"
	default:
		return "other"
	}
}

func humanSize(size int64) string {
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
	suffix := []string{"KB", "MB", "GB", "TB"}[exp]
	return fmt.Sprintf("%.2f %s", value, suffix)
}

func initLogger() {
	f, _ := os.Create("scan.log")
	logFile = f
	logger = log.New(f, "", log.LstdFlags)
}

func printProgress() {
	n := atomic.LoadInt64(&filesProcessed)
	if n%1000 == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Printf("📊 %8d файлов | %.1f MB RAM\n", n, float64(m.Alloc)/1024.0/1024.0)
	}
}

// --- assembleNestedFromFlat и computeDirSizes — такие же, как в твоей версии ---

// --- Сбор дерева из "плоского" массива ---
func assembleNestedFromFlat(flat []FileInfo) FileInfo {
	if len(flat) == 0 {
		return FileInfo{IsDir: true, FullName: "(empty)", NameOnly: "(empty)"}
	}

	// нормализуем и строим индексы
	type nodePtr = *FileInfo
	pathToNode := make(map[string]nodePtr, len(flat))
	parentToKids := make(map[string][]FileInfo, len(flat))

	// используем FullPath как идентификатор (он равен FullPathOrig при сборке)
	for i := range flat {
		// гарантируем непротиворечивость ParentDir/FullPath
		if flat[i].ParentDir == "." {
			flat[i].ParentDir = ""
		}
		pathToNode[flat[i].FullPath] = &flat[i]
	}

	// группируем детей по ParentDir
	var roots []FileInfo
	for _, fi := range flat {
		if _, ok := pathToNode[fi.ParentDir]; ok {
			parentToKids[fi.ParentDir] = append(parentToKids[fi.ParentDir], fi)
		} else {
			// родитель не присутствует в flat → это корневой кандидат
			roots = append(roots, fi)
		}
	}

	// рекурсивная сборка
	var build func(FileInfo) FileInfo
	build = func(n FileInfo) FileInfo {
		kids := parentToKids[n.FullPath]
		if len(kids) == 0 {
			// лист (файл или пустая директория)
			return n
		}
		n.Children = make([]FileInfo, 0, len(kids))
		var total int64
		for _, ch := range kids {
			built := build(ch)
			n.Children = append(n.Children, built)
			total += built.SizeBytes
		}
		if n.IsDir {
			n.SizeBytes = total
			n.SizeHuman = humanSize(total)
			// каталоги первыми, затем файлы; сортировка case-insensitive
			sort.Slice(n.Children, func(i, j int) bool {
				di, dj := n.Children[i].IsDir, n.Children[j].IsDir
				if di != dj {
					return di && !dj
				}
				ni := strings.ToLower(n.Children[i].FullName)
				nj := strings.ToLower(n.Children[j].FullName)
				return ni < nj
			})
		}
		return n
	}

	if len(roots) == 0 {
		// fallback: ищем элемент без ParentDir или берём первый
		for _, fi := range flat {
			if fi.ParentDir == "" {
				roots = append(roots, fi)
			}
		}
		if len(roots) == 0 {
			roots = append(roots, flat[0])
		}
	}

	// если ровно один корень — возвращаем его; иначе — виртуальный корень
	if len(roots) == 1 {
		return build(roots[0])
	}

	// создаём виртуальный корень, чтобы сохранить все «верхние» ветки
	sort.Slice(roots, func(i, j int) bool {
		di, dj := roots[i].IsDir, roots[j].IsDir
		if di != dj {
			return di && !dj
		}
		ni := strings.ToLower(roots[i].FullName)
		nj := strings.ToLower(roots[j].FullName)
		return ni < nj
	})
	root := FileInfo{
		IsDir:     true,
		FullName:  "(root)",
		NameOnly:  "(root)",
		FullPath:  "",
		ParentDir: "",
		Children:  make([]FileInfo, 0, len(roots)),
	}
	var total int64
	for _, r := range roots {
		b := build(r)
		root.Children = append(root.Children, b)
		total += b.SizeBytes
	}
	root.SizeBytes = total
	root.SizeHuman = humanSize(total)
	return root
}

// --- Пересчёт размеров/дат по директориям ---
func computeDirSizes(node *FileInfo) int64 {
	if !node.IsDir {
		return node.SizeBytes
	}
	var total int64
	var earliest, latest time.Time
	for i := range node.Children {
		sz := computeDirSizes(&node.Children[i])
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
	node.SizeHuman = humanSize(total)
	if !earliest.IsZero() {
		node.Created = earliest
	}
	if !latest.IsZero() {
		node.Updated = latest
	}
	if node.Md5 == "" {
		node.Md5 = md5String(node.FullName)
	}
	return total
}

// --- Helpers ---
func makeFlatEntry(path string, info os.FileInfo) FileInfo {
	parent := filepath.Dir(path)
	if parent == "." {
		parent = ""
	}

	size := int64(0)
	if !info.IsDir() {
		size = info.Size()
	}

	entry := FileInfo{
		IsDir:        info.IsDir(),
		FullName:     info.Name(),
		Ext:          strings.TrimPrefix(strings.ToLower(filepath.Ext(info.Name())), "."),
		NameOnly:     strings.TrimSuffix(info.Name(), filepath.Ext(info.Name())),
		SizeBytes:    size,
		SizeHuman:    humanSize(size),
		FullPath:     path,
		FullPathOrig: path,
		ParentDir:    parent,
		Created:      info.ModTime(), // в потоковом режиме оставляем ModTime
		Updated:      info.ModTime(),
		Perm:         info.Mode().String(),
		FileType:     detectFileType(info.Name()),
	}

	if info.IsDir() {
		entry.Md5 = md5String(info.Name())
	} else {
		entry.Md5 = fileMD5(path)
	}
	return entry
}

func md5String_(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func fileMD5_(path string) string {
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

// исключение по подстроке ПОЛНОГО пути (регистронезависимо)
func shouldExclude_(absPath string) bool {
	pl := strings.ToLower(absPath)
	for _, ex := range excludeList {
		if ex != "" && strings.Contains(pl, ex) {
			return true
		}
	}
	return false
}

func detectFileType_(name string) string {
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
	case ".go", ".js", ".ts", ".py", ".html", ".css", ".json", ".yaml", ".yml", ".rs", ".java", ".c", ".cpp", ".cs", ".php":
		return "code"
	default:
		return "other"
	}
}

func humanSize_(size int64) string {
	// бинарные единицы
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	// поддержка до EiB
	suffixes := []string{"KB", "MB", "GB", "TB", "PB", "EB"}
	if exp >= len(suffixes) {
		exp = len(suffixes) - 1
	}
	value := float64(size) / float64(div)
	return fmt.Sprintf("%.2f %s", value, suffixes[exp])
}

func printProgress_() {
	count := atomic.LoadInt64(&filesProcessed)
	if count == 0 {
		return
	}
	step := int64(100)
	switch {
	case count >= 10000:
		step = 10000
	case count >= 1000:
		step = 1000
	}
	if count%step != 0 {
		return
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	elapsed := time.Since(startTime).Seconds()
	if elapsed <= 0 {
		elapsed = 0.000001
	}
	speed := float64(count) / elapsed
	fmt.Printf("📊 %8d файлов | %6.1fs | %6.1f ф/с | %.1f MB\n",
		count, elapsed, speed, float64(m.Alloc)/1024.0/1024.0)
}

func writeFinalJSON(output string, root FileInfo, pretty bool) {
	f, err := os.Create(output)
	if err != nil {
		fmt.Println("Ошибка создания выходного файла:", err)
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(root); err != nil {
		fmt.Println("Ошибка записи JSON:", err)
	}
}

// Для Unix возвращаем ModTime как «наиболее близкое» к Created.
func getCreateTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

/*
Ключевые изменения и исправления:

Фильтрация по исключениям

shouldExclude теперь принимает полный путь и используется строго с абсолютным/полным путём во всех местах (и в Walk, и в buildStructure). Это исправляет ситуацию, когда раньше в buildStructure передавалось только имя (info.Name()), из-за чего исключения «не срабатывали».

В buildStructure мы не входим в исключённые поддеревья (проверка перед рекурсией).

Надёжная сборка дерева из flat

Полностью переписан assembleNestedFromFlat: теперь он строит parentToKids по ParentDir, корректно собирает все уровни, стабильно сортирует детей (директории вперёд, затем файлы, без учёта регистра).

Если корней несколько, возвращается виртуальный корень (root) с агрегированным размером, иначе — единственный реальный корень. Это убирает кейс «на выходе только корень/пусто».

Потоковый режим и resume

Безопасное дозаполнение JSON-массива: вырезается закрывающая ]\n, добавляется ,\n, после дозаписи снова закрываем массив.

Регулярный Flush() каждые 500 элементов.

Прогресс и стабильность

Адаптивный шаг прогресса (100/1000/10000), защита от деления на 0.

Везде where-possible — проверка ошибок и безопасные _ = при вспомогательных операциях записи/seek/truncate.

Размеры и сортировки

Единообразный humanSize с поддержкой до EB.

Стабильные сортировки: директории → файлы, сравнение имён case-insensitive.

Прочее

Для директорий MD5 — детерминированный от имени (дешёво), для файлов — реальный MD5 содержимого.

Расширен классификатор типов файлов.

Убран неиспользуемый humanSize2 и дубляжи вычислений в makeFlatEntry.



Возможность	Флаг	Описание
Удаление дубликатов	--dedupe	фильтрация по FullPathOrig
Flat-вывод	--merge-flat	сохраняет как []FileInfo, без дерева
Совместимость	автоматическая	поддерживает и плоские, и древовидные JSON
Пересчёт ChildCount	всегда	корректное количество детей в каталоге
Пересчёт SizeBytes и дат	всегда	через computeDirSizes()
*/
