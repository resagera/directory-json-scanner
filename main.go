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
	excludeFlag = flag.String("exclude", "", "Исключения через запятую")
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
	fmt.Println("test6")
	flag.Parse()
	startTime = time.Now()
	initLogger()

	if *excludeFlag != "" {
		for _, e := range strings.Split(*excludeFlag, ",") {
			e = strings.TrimSpace(e)
			if e != "" {
				excludeList = append(excludeList, strings.ToLower(e))
			}
		}
	}

	streamTempName = strings.TrimSuffix(*outputFlag, ".json") + "_temp.json"

	// Режим объединения
	if *mergeFlag != "" {
		mergeMode()
		return
	}

	if !*streamFlag {
		processNormal()
		return
	}

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
			streamFileHandle.Truncate(0)
			streamFileHandle.Seek(0, 0)
			streamFileHandle.WriteString("[\n")
		}
		streamWriter = bufio.NewWriter(streamFileHandle)
	}

	fmt.Printf("📁 Начало сканирования: %s\n", *dirFlag)
	err := filepath.Walk(*dirFlag, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		//fmt.Println("SCAN:", path)
		if shouldExclude(path, info) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		abs, _ := filepath.Abs(path)
		if *resumeFlag && existingPaths != nil {
			if _, exists := existingPaths[abs]; exists {
				return nil
			}
		}

		entry := makeFlatEntry(abs, info)
		//fmt.Println("SCAN entry:", entry)
		if *streamFlag {
			b, _ := json.Marshal(entry)
			if atomic.LoadInt64(&filesProcessed) > 0 || len(existingPaths) > 0 {
				streamWriter.WriteString(",\n")
			}
			streamWriter.Write(b)
			if atomic.AddInt64(&filesProcessed, 1)%500 == 0 {
				streamWriter.Flush()
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
		streamWriter.WriteString("\n]\n")
		streamWriter.Flush()
		streamFileHandle.Close()
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
	all := []FileInfo{}
	for _, file := range files {
		file = strings.TrimSpace(file)
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

	// Подготовка списка исключений
	excludes := strings.Split(*excludeFlag, ",")
	for _, e := range excludes {
		if e == "" {
			continue
		}
		fmt.Println("exclude", e, strings.ToLower(strings.TrimSpace(e)))
		excludeList = append(excludeList, strings.ToLower(strings.TrimSpace(e)))
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

func buildStructure(path string, info os.FileInfo) FileInfo {
	name := info.Name()
	if shouldExclude(name, info) {
		return FileInfo{} // пропускаем элемент
	}

	count := atomic.AddInt64(&filesProcessed, 1)

	// Определяем шаг прогресса
	step := int64(100)
	switch {
	case count >= 10000:
		step = 10000
	case count >= 1000:
		step = 1000
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
		FullName:     name,
		Ext:          strings.TrimPrefix(filepath.Ext(name), "."),
		NameOnly:     strings.TrimSuffix(name, filepath.Ext(name)),
		FullPath:     path,
		FullPathOrig: path,
		ParentDir:    parent,
		Created:      getCreateTime(path),
		Updated:      info.ModTime(),
		Perm:         info.Mode().String(),
		FileType:     detectFileType(name),
	}

	if info.IsDir() {
		var totalSize int64
		entries, _ := os.ReadDir(path)
		for _, e := range entries {
			childInfo, err := e.Info()
			if err != nil {
				continue
			}
			child := buildStructure(filepath.Join(path, e.Name()), childInfo)
			if child.FullName == "" {
				continue // пропущен
			}
			entry.Children = append(entry.Children, child)
			totalSize += child.SizeBytes
		}
		entry.SizeBytes = totalSize
		entry.SizeHuman = humanSize(totalSize)
		entry.Md5 = md5String(info.Name()) // для папок просто имя
	} else {
		size := info.Size()
		entry.SizeBytes = size
		entry.SizeHuman = humanSize(size)
		entry.Md5 = fileMD5(path)
	}
	printProgress()

	return entry
}

// --- Logger ---
func initLogger() {
	logFile, _ = os.Create("scan.log")
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
		m[f.FullPathOrig] = struct{}{}
	}
	return m
}

func appendToExistingJSON(f *os.File) {
	stat, _ := f.Stat()
	if stat.Size() < 3 {
		return
	}
	offset := stat.Size() - 2
	f.Seek(offset, 0)
	f.Truncate(offset)
	f.WriteString(",\n")
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

// --- Tree Assembling ---
func assembleNestedFromFlat_(flat []FileInfo) FileInfo {
	nodes := map[string]*FileInfo{}
	var root FileInfo

	// сохраняем каждый элемент корректно
	for _, f := range flat {
		item := f // создаём копию
		p := filepath.Clean(item.FullPathOrig)
		nodes[p] = &item
	}

	// связываем файлы с родителями
	for _, f := range flat {
		if f.IsDir {
			continue
		}
		dir := filepath.Dir(f.FullPathOrig)
		for dir != "" {
			parent, ok := nodes[dir]
			if !ok {
				parent = &FileInfo{
					IsDir:        true,
					FullName:     filepath.Base(dir),
					NameOnly:     filepath.Base(dir),
					FullPath:     dir,
					FullPathOrig: dir,
				}
				nodes[dir] = parent
			}
			parent.Children = append(parent.Children, f)
			dir = filepath.Dir(dir)
			if dir == "." || dir == "/" {
				break
			}
		}
	}

	// выбираем корневые папки
	for _, v := range nodes {
		if filepath.Dir(v.FullPathOrig) == "." || filepath.Dir(v.FullPathOrig) == "" {
			root.Children = append(root.Children, *v)
		}
	}

	// сортируем для стабильности
	sort.Slice(root.Children, func(i, j int) bool {
		return root.Children[i].FullName < root.Children[j].FullName
	})

	return root
}

func normalizePath(p string) string {
	if p == "" {
		return p
	}
	// Удаляем завершающий слэш, если не корень
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		return strings.TrimSuffix(p, "/")
	}
	return p
}

func assembleNestedFromFlat(flat []FileInfo) FileInfo {
	if len(flat) == 0 {
		return FileInfo{}
	}

	// Карта по полному пути и группировка детей по родителю.
	byPath := make(map[string]FileInfo, len(flat))
	childrenOf := make(map[string][]FileInfo, len(flat))

	for _, fi := range flat {
		byPath[fi.FullPath] = fi
	}

	var roots []FileInfo
	for _, fi := range flat {
		if _, ok := byPath[fi.ParentDir]; ok {
			childrenOf[fi.ParentDir] = append(childrenOf[fi.ParentDir], fi)
		} else {
			// Родителя нет во входном массиве → кандидат в корень.
			roots = append(roots, fi)
		}
	}

	// Если несколько корней — берём первый. При желании можно вернуть []FileInfo.
	if len(roots) == 0 {
		// fallback: выберем тот, чей ParentDir равен "" (на всякий случай)
		for _, fi := range flat {
			if fi.ParentDir == "" {
				roots = append(roots, fi)
			}
		}
		if len(roots) == 0 {
			// крайний случай — вернём первый элемент
			roots = append(roots, flat[0])
		}
	}
	root := buildTree(roots[0], childrenOf)

	return root
}

func buildTree(node FileInfo, childrenOf map[string][]FileInfo) FileInfo {
	kids := childrenOf[node.FullPath]

	// Рекурсивно собрать детей.
	node.Children = make([]FileInfo, 0, len(kids))
	var total int64
	for _, ch := range kids {
		built := buildTree(ch, childrenOf)
		node.Children = append(node.Children, built)
		total += built.SizeBytes
	}

	// Если директория — пересчитать размер как сумму детей.
	if node.IsDir {
		node.SizeBytes = total
		node.SizeHuman = humanSize(total)
		// Стабильная сортировка: каталоги первыми, затем файлы, по имени без регистра.
		sort.Slice(node.Children, func(i, j int) bool {
			di, dj := node.Children[i].IsDir, node.Children[j].IsDir
			if di != dj {
				return di && !dj
			}
			ni := strings.ToLower(node.Children[i].FullName)
			nj := strings.ToLower(node.Children[j].FullName)
			return ni < nj
		})
	}
	return node
}

// humanSize возвращает человекочитаемую строку размера
func humanSize2(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func assembleNestedFromFlat__(flat []FileInfo) FileInfo {
	if len(flat) == 0 {
		return FileInfo{IsDir: true, FullPath: "", Children: nil}
	}

	// Нормализуем пути во всех элементах и работаем с копией
	items := make([]FileInfo, len(flat))
	for i, item := range flat {
		item.FullPath = normalizePath(item.FullPath)
		item.ParentDir = normalizePath(item.ParentDir)
		items[i] = item
	}

	// Карта нормализованных FullPath -> указатель на элемент
	pathToNode := make(map[string]*FileInfo)
	for i := range items {
		pathToNode[items[i].FullPath] = &items[i]
	}

	// Для каждой директории собираем всех детей (файлы и папки)
	for i := range items {
		if !items[i].IsDir {
			continue
		}
		parentPath := items[i].FullPath
		for j := range items {
			if items[j].ParentDir == parentPath {
				items[i].Children = append(items[i].Children, items[j])
			}
		}
		sort.Slice(items[i].Children, func(a, b int) bool {
			aIsDir, bIsDir := items[i].Children[a].IsDir, items[i].Children[b].IsDir
			if aIsDir == bIsDir {
				return items[i].Children[a].FullName < items[i].Children[b].FullName
			}
			return aIsDir
		})
	}

	// Находим корни: ParentDir не существует в FullPath
	var roots []FileInfo
	for i := range items {
		if _, exists := pathToNode[items[i].ParentDir]; !exists {
			roots = append(roots, items[i])
		}
	}

	sort.Slice(roots, func(i, j int) bool {
		aIsDir, bIsDir := roots[i].IsDir, roots[j].IsDir
		if aIsDir == bIsDir {
			return roots[i].FullName < roots[j].FullName
		}
		return aIsDir
	})

	if len(roots) == 1 {
		return roots[0]
	}

	return FileInfo{
		IsDir:     true,
		FullName:  "(root)",
		NameOnly:  "(root)",
		FullPath:  "",
		ParentDir: "",
		Children:  roots,
	}
}

// --- Directory Size Calculation ---
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
	node.Created = earliest
	node.Updated = latest
	node.Md5 = md5String(node.FullName)
	return total
}

// --- Helpers ---
func makeFlatEntry(path string, info os.FileInfo) FileInfo {
	size := int64(0)
	if !info.IsDir() {
		size = info.Size()
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
		SizeBytes:    size,
		SizeHuman:    humanSize(size),
		FullPath:     path,
		FullPathOrig: path,
		ParentDir:    parent, // ✅ заполняем
		Created:      info.ModTime(),
		Updated:      info.ModTime(),
		Perm:         info.Mode().String(),
		Md5:          md5String(info.Name()),
		FileType:     detectFileType(info.Name()),
	}

	if info.IsDir() {
		entry.Md5 = md5String(info.Name()) // для папок просто имя
	} else {
		size := info.Size()
		entry.SizeBytes = size
		entry.SizeHuman = humanSize(size)
		entry.Md5 = fileMD5(path)
	}

	return entry
}

func md5String(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// Вычисляет MD5 файла
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

func shouldExclude(path string, info os.FileInfo) bool {
	pl := strings.ToLower(path)
	for _, ex := range excludeList {
		if strings.Contains(pl, ex) {
			return true
		}
	}
	return false
}

func detectFileType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif":
		return "image"
	case ".mp4", ".avi", ".mkv":
		return "video"
	case ".mp3", ".wav", ".flac":
		return "audio"
	case ".txt", ".md", ".log":
		return "text"
	case ".go", ".js", ".py", ".html", ".css", ".json":
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

func printProgress() {
	count := atomic.LoadInt64(&filesProcessed)
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

// Получает примерную дату создания (на Unix системах)
func getCreateTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	stat := info.Sys()
	if stat == nil {
		return info.ModTime()
	}
	return info.ModTime()
}
