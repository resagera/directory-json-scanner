// main.go
package main

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
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

	// режим объединения
	if *mergeFlag != "" {
		mergeMode()
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
		entry := makeFlatEntry(abs, info)

		if *streamFlag {
			// потоковый режим
			b, _ := json.Marshal(entry)
			if atomic.LoadInt64(&filesProcessed) > 0 {
				streamWriter.WriteString(",\n")
			}
			streamWriter.Write(b)
		}

		// 🔧 добавь эту строку ↓
		atomic.AddInt64(&filesProcessed, 1)

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
		writeFinalJSON(*outputFlag, root, *prettyFlag)
		fmt.Printf("🎉 Результат собран: %s\n", *outputFlag)
	}
	logger.Printf("Готово.")
}

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
	writeFinalJSON(*outputFlag, root, *prettyFlag)
	fmt.Println("✅ Объединение завершено.")
}

func initLogger() {
	logFile, _ = os.Create("scan.log")
	logger = log.New(logFile, "", log.LstdFlags)
}

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

// --- Вложенная сборка ---
func assembleNestedFromFlat(flat []FileInfo) FileInfo {
	nodes := map[string]*FileInfo{}
	var root FileInfo
	for _, f := range flat {
		p := filepath.Clean(f.FullPathOrig)
		nodes[p] = &f
	}

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

	for _, v := range nodes {
		if filepath.Dir(v.FullPathOrig) == "." {
			root.Children = append(root.Children, *v)
		}
	}
	return root
}

// --- Вспомогательные функции ---
func makeFlatEntry(path string, info os.FileInfo) FileInfo {
	size := int64(0)
	if !info.IsDir() {
		size = info.Size()
	}
	return FileInfo{
		IsDir:        info.IsDir(),
		FullName:     info.Name(),
		Ext:          strings.TrimPrefix(filepath.Ext(info.Name()), "."),
		NameOnly:     strings.TrimSuffix(info.Name(), filepath.Ext(info.Name())),
		SizeBytes:    size,
		SizeHuman:    humanSize(size),
		FullPath:     path,
		FullPathOrig: path,
		Created:      info.ModTime(),
		Updated:      info.ModTime(),
		Perm:         info.Mode().String(),
		Md5:          md5String(info.Name()),
		FileType:     detectFileType(info.Name()),
	}
}

func md5String(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
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
