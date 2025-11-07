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
	Children     []FileInfo `json:"Children,omitempty"`
}

var (
	dirFlag     = flag.String("dir", ".", "Директория для сканирования")
	excludeFlag = flag.String("exclude", "", "Исключения через запятую (подстроки путей, регистронезависимо)")
	outputFlag  = flag.String("output", "structure.json", "Выходной JSON-файл")
	prettyFlag  = flag.Bool("pretty", false, "Форматировать JSON красиво")
	streamFlag  = flag.Bool("stream", false, "Потоковая запись в temp")
	resumeFlag  = flag.Bool("resume", false, "Продолжить сканирование (только с --stream)")
	mergeFlag   = flag.String("merge", "", "Список JSON-файлов через запятую для объединения")
)

var (
	excludeList      []string
	streamTempName   string
	existingPaths    map[string]struct{}
	filesProcessed   int64
	startTime        time.Time
	logger           *log.Logger
	logFile          *os.File
	streamWriter     *bufio.Writer
	streamFileHandle *os.File
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

	// подготавливаем исключения
	if *excludeFlag != "" {
		for _, e := range strings.Split(*excludeFlag, ",") {
			e = strings.TrimSpace(e)
			if e != "" {
				excludeList = append(excludeList, strings.ToLower(e))
			}
		}
	}

	streamTempName = strings.TrimSuffix(*outputFlag, ".json") + "_temp.json"

	// режим объединения заранее сформированных flat JSON-ов
	if *mergeFlag != "" {
		mergeMode()
		return
	}

	// обычный режим (без стрима)
	if !*streamFlag {
		processNormal()
		return
	}

	// потоковый режим с возможностью resume
	if *streamFlag {
		if *resumeFlag {
			existingPaths = loadExistingTempFlatList(streamTempName)
			fmt.Printf("🔁 Режим resume: найдено %d уже обработанных файлов\n", len(existingPaths))
		}

		var err error
		streamFileHandle, err = os.OpenFile(streamTempName, os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			log.Fatalf("Ошибка открытия temp файла: %v", err)
		}
		if *resumeFlag && len(existingPaths) > 0 {
			appendToExistingJSON(streamFileHandle)
		} else {
			_ = streamFileHandle.Truncate(0)
			_, _ = streamFileHandle.Seek(0, 0)
			_, _ = streamFileHandle.WriteString("[\n")
		}
		streamWriter = bufio.NewWriter(streamFileHandle)
	}

	rootDirAbs, _ := filepath.Abs(*dirFlag)
	fmt.Printf("📁 Начало сканирования: %s\n", rootDirAbs)

	// Walk со строгим исключением целых поддеревьев
	err := filepath.Walk(*dirFlag, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// пропускаем проблемные элементы, но не рушим проход
			return nil
		}
		abs, _ := filepath.Abs(path)

		// исключения — по полному пути
		if shouldExclude(abs) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// в режиме resume пропускаем уже записанные
		if *resumeFlag && existingPaths != nil {
			if _, exists := existingPaths[abs]; exists {
				return nil
			}
		}

		entry := makeFlatEntry(abs, info)

		if *streamFlag {
			b, _ := json.Marshal(entry)
			// добавляем запятую, если это не первый элемент
			if atomic.LoadInt64(&filesProcessed) > 0 || len(existingPaths) > 0 {
				_, _ = streamWriter.WriteString(",\n")
			}
			_, _ = streamWriter.Write(b)
			// флашим периодически
			if atomic.AddInt64(&filesProcessed, 1)%500 == 0 {
				_ = streamWriter.Flush()
			}
		} else {
			atomic.AddInt64(&filesProcessed, 1)
		}
		printProgress()
		return nil
	})
	if err != nil {
		log.Printf("Ошибка обхода: %v", err)
	}

	if *streamFlag {
		_, _ = streamWriter.WriteString("\n]\n")
		_ = streamWriter.Flush()
		_ = streamFileHandle.Close()
		fmt.Printf("✅ Записан temp: %s\n", streamTempName)
		logger.Printf("Temp file saved: %s", streamTempName)

		flat, err := readFlatArrayFromFile(streamTempName)
		if err != nil {
			log.Fatalf("Ошибка чтения temp: %v", err)
		}
		root := assembleNestedFromFlat(flat)
		computeDirSizes(&root)
		writeFinalJSON(*outputFlag, root, *prettyFlag)
		fmt.Printf("🎉 Результат собран: %s\n", *outputFlag)
	}
	logger.Printf("Готово.")
}

// --- Merge Mode ---
func mergeMode() {
	files := strings.Split(*mergeFlag, ",")
	fmt.Printf("🔗 Объединение %d файлов...\n", len(files))
	var all []FileInfo
	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("Ошибка чтения %s: %v\n", file, err)
			continue
		}
		var flat []FileInfo
		if err := json.Unmarshal(data, &flat); err != nil {
			fmt.Printf("Ошибка парсинга %s: %v\n", file, err)
			continue
		}
		all = append(all, flat...)
	}
	root := assembleNestedFromFlat(all)
	computeDirSizes(&root)
	writeFinalJSON(*outputFlag, root, *prettyFlag)
	fmt.Println("✅ Объединение завершено.")
}

// --- Обычный (нестримовый) режим ---
func processNormal() {
	root, err := filepath.Abs(*dirFlag)
	if err != nil {
		fmt.Println("Ошибка получения пути:", err)
		return
	}

	outputPath, err := filepath.Abs(*outputFlag)
	if err != nil {
		fmt.Println("Ошибка определения пути для вывода:", err)
		return
	}

	fmt.Println("📁 Исходная директория:", root)
	fmt.Println("💾 Результат будет сохранён в:", outputPath)
	fmt.Println("⏳ Начинаем сканирование...\n")

	// Подготовка списка исключений (на всякий случай — если передали с пробелами)
	for _, e := range strings.Split(*excludeFlag, ",") {
		e = strings.TrimSpace(e)
		if e != "" {
			excludeList = append(excludeList, strings.ToLower(e))
		}
	}

	info, err := os.Stat(root)
	if err != nil {
		fmt.Println("Ошибка чтения директории:", err)
		return
	}

	startTime = time.Now()
	result := buildStructure(root, info)

	fmt.Printf("\n✅ Сканирование завершено. Всего обработано: %d элементов.\n", atomic.LoadInt64(&filesProcessed))
	fmt.Printf("🕒 Время выполнения: %v\n", time.Since(startTime))

	file, err := os.Create(outputPath)
	if err != nil {
		fmt.Println("Ошибка создания JSON файла:", err)
		return
	}
	defer file.Close()

	if *prettyFlag {
		enc := json.NewEncoder(file)
		enc.SetIndent("", "  ")
		err = enc.Encode(result)
	} else {
		data, _ := json.Marshal(result)
		_, err = file.Write(data)
	}

	if err != nil {
		fmt.Println("Ошибка записи JSON:", err)
		return
	}

	fmt.Println("🎉 JSON структура успешно сохранена в:", outputPath)
}

// Рекурсивный сбор структуры (нестримовый)
func buildStructure(path string, info os.FileInfo) FileInfo {
	// важно: фильтруем по ПОЛНОМУ пути
	if shouldExclude(path) {
		return FileInfo{}
	}

	count := atomic.AddInt64(&filesProcessed, 1)

	// адаптивный шаг прогресса
	step := int64(10)
	switch {
	case count >= 10000:
		step = 10000
	case count >= 1000:
		step = 1000
	case count >= 100:
		step = 100
	}
	if count%step == 0 {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		elapsed := time.Since(startTime).Truncate(time.Millisecond)
		fmt.Printf("... обработано %d элементов | память: %.2f MB | прошло: %v\n",
			count, float64(mem.Alloc)/1024.0/1024.0, elapsed)
	}

	parent := filepath.Dir(path)
	if parent == "." {
		parent = ""
	}

	entry := FileInfo{
		IsDir:        info.IsDir(),
		FullName:     info.Name(),
		Ext:          strings.TrimPrefix(filepath.Ext(info.Name()), "."),
		NameOnly:     strings.TrimSuffix(info.Name(), filepath.Ext(info.Name())),
		FullPath:     path,
		FullPathOrig: path,
		ParentDir:    parent,
		Created:      getCreateTime(path), // максимально близко к "created" для Unix
		Updated:      info.ModTime(),
		Perm:         info.Mode().String(),
		FileType:     detectFileType(info.Name()),
	}

	if info.IsDir() {
		var totalSize int64
		entries, _ := os.ReadDir(path)
		for _, e := range entries {
			childPath := filepath.Join(path, e.Name())
			// не входим в исключённые поддеревья
			if shouldExclude(childPath) {
				continue
			}
			childInfo, err := e.Info()
			if err != nil {
				continue
			}
			child := buildStructure(childPath, childInfo)
			if child.FullName == "" {
				continue // пропущен
			}
			entry.Children = append(entry.Children, child)
			totalSize += child.SizeBytes
		}
		entry.SizeBytes = totalSize
		entry.SizeHuman = humanSize(totalSize)
		entry.Md5 = md5String(info.Name()) // для папок — детерминированный псевдо-хэш по имени
		// каталоги первыми, затем файлы; сортировка case-insensitive
		sort.Slice(entry.Children, func(i, j int) bool {
			di, dj := entry.Children[i].IsDir, entry.Children[j].IsDir
			if di != dj {
				return di && !dj
			}
			ni := strings.ToLower(entry.Children[i].FullName)
			nj := strings.ToLower(entry.Children[j].FullName)
			return ni < nj
		})
	} else {
		size := info.Size()
		entry.SizeBytes = size
		entry.SizeHuman = humanSize(size)
		entry.Md5 = fileMD5(path) // реальный MD5 только для файлов
	}
	printProgress()
	return entry
}

// --- Logger ---
func initLogger() {
	var err error
	logFile, err = os.Create("scan.log")
	if err != nil {
		log.Printf("Не удалось создать scan.log: %v", err)
		return
	}
	logger = log.New(logFile, "", log.LstdFlags)
}

// --- Resume Support ---
func loadExistingTempFlatList(tempPath string) map[string]struct{} {
	data, err := os.ReadFile(tempPath)
	if err != nil {
		return map[string]struct{}{}
	}
	var arr []FileInfo
	if err := json.Unmarshal(data, &arr); err != nil {
		fmt.Printf("⚠️ Ошибка чтения temp, начнем заново: %v\n", err)
		return map[string]struct{}{}
	}
	m := make(map[string]struct{}, len(arr))
	for _, f := range arr {
		if f.FullPathOrig != "" {
			m[f.FullPathOrig] = struct{}{}
		}
	}
	return m
}

func appendToExistingJSON(f *os.File) {
	stat, _ := f.Stat()
	if stat.Size() < 3 {
		return
	}
	// отрезаем закрывающую скобку массива "]\n"
	offset := stat.Size() - 2
	_, _ = f.Seek(offset, 0)
	_ = f.Truncate(offset)
	_, _ = f.WriteString(",\n")
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

func md5String(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func fileMD5(path string) string {
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
func shouldExclude(absPath string) bool {
	pl := strings.ToLower(absPath)
	for _, ex := range excludeList {
		if ex != "" && strings.Contains(pl, ex) {
			return true
		}
	}
	return false
}

func detectFileType(name string) string {
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

func humanSize(size int64) string {
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

func printProgress() {
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
*/
