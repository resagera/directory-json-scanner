package main

import (
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
	"strings"
	"sync"
	"time"
)

type FileInfo struct {
	IsDir        bool      `json:"IsDir"`
	FullName     string    `json:"FullName"`
	Ext          string    `json:"Ext"`
	NameOnly     string    `json:"NameOnly"`
	SizeBytes    int64     `json:"SizeBytes"`
	SizeHuman    string    `json:"SizeHuman"`
	FullPath     string    `json:"FullPath"`
	FullPathOrig string    `json:"FullPathOrig"`
	Created      time.Time `json:"Created"`
	Updated      time.Time `json:"Updated"`
	Perm         string    `json:"Perm"`
	Md5          string    `json:"Md5"`
	FileType     string    `json:"FileType"`
}

var (
	dirFlag     = flag.String("dir", ".", "Директория для сканирования")
	excludeFlag = flag.String("exclude", "", "Список исключений через запятую (файлы или папки)")
	outputFlag  = flag.String("output", "result.json", "Имя выходного JSON файла")
	prettyFlag  = flag.Bool("pretty", false, "Форматировать JSON красиво")
	streamFlag  = flag.Bool("stream", false, "Потоковая запись элементов в temp-файл")
	mergeFlag   = flag.String("merge", "", "Список JSON файлов через запятую для объединения (в этом режиме сканирование не выполняется)")
)

var (
	excluded   []string
	startTime  time.Time
	count      int64
	totalSize  int64
	mutex      sync.Mutex
	logFile    *os.File
	logger     *log.Logger
	lastReport int64 = 100
)

func main() {
	flag.Parse()
	startTime = time.Now()

	initLog()

	// --- Новый режим объединения JSON-файлов ---
	if *mergeFlag != "" {
		files := strings.Split(*mergeFlag, ",")
		fmt.Printf("🔗 Объединение %d JSON файлов...\n", len(files))
		logger.Printf("Начато объединение %d файлов", len(files))
		mergeJSONFiles(files, *outputFlag)
		fmt.Printf("✅ Объединённый файл сохранён в: %s\n", absPath(*outputFlag))
		return
	}

	// --- Обычный режим сканирования ---
	excluded = strings.Split(*excludeFlag, ",")
	fmt.Printf("📁 Сканирование директории: %s\n", *dirFlag)
	fmt.Printf("📄 Выходной файл: %s\n", absPath(*outputFlag))

	if *streamFlag {
		processStreamed()
	} else {
		processNormal()
	}

	fmt.Println("✅ Готово!")
	logger.Println("✅ Готово!")
}

func processStreamed() {
	tempFile := strings.TrimSuffix(*outputFlag, ".json") + "_temp.json"
	f, err := os.Create(tempFile)
	if err != nil {
		log.Fatalf("Ошибка создания temp файла: %v", err)
	}
	defer f.Close()

	f.WriteString("[\n")

	first := true
	fileChan := make(chan *FileInfo, 100)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := filepath.Walk(*dirFlag, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if shouldExclude(path) {
				return filepath.SkipDir
			}

			fi := buildFileInfo(path, info)
			fileChan <- fi
			updateProgress()
			return nil
		})
		if err != nil {
			log.Println("Ошибка обхода:", err)
		}
		close(fileChan)
	}()

	for fi := range fileChan {
		data, _ := json.Marshal(fi)
		if !first {
			f.WriteString(",\n")
		}
		first = false
		f.Write(data)
	}
	wg.Wait()
	f.WriteString("\n]")

	fmt.Printf("🔧 Сборка итогового файла %s...\n", *outputFlag)
	assembleFinalFile(tempFile, *outputFlag)
	os.Remove(tempFile)
}

func assembleFinalFile(tempPath, finalPath string) {
	input, err := os.ReadFile(tempPath)
	if err != nil {
		log.Fatalf("Ошибка чтения temp файла: %v", err)
	}

	var arr []FileInfo
	if err := json.Unmarshal(input, &arr); err != nil {
		log.Fatalf("Ошибка при разборе temp файла: %v", err)
	}

	writeJSON(finalPath, arr)
}

func processNormal() {
	var files []FileInfo

	filepath.Walk(*dirFlag, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if shouldExclude(path) {
			return filepath.SkipDir
		}

		fi := buildFileInfo(path, info)
		files = append(files, *fi)
		updateProgress()
		return nil
	})

	writeJSON(*outputFlag, files)
}

func mergeJSONFiles(inputs []string, output string) {
	var merged []FileInfo

	for _, file := range inputs {
		file = strings.TrimSpace(file)
		fmt.Printf("📖 Чтение: %s\n", file)
		data, err := os.ReadFile(file)
		if err != nil {
			log.Printf("Ошибка чтения %s: %v\n", file, err)
			continue
		}
		var arr []FileInfo
		if err := json.Unmarshal(data, &arr); err != nil {
			log.Printf("Ошибка парсинга %s: %v\n", file, err)
			continue
		}
		merged = append(merged, arr...)
		fmt.Printf("  ➕ Добавлено %d элементов (всего %d)\n", len(arr), len(merged))
	}

	writeJSON(output, merged)
	logger.Printf("Успешно объединено %d файлов", len(inputs))
}

func writeJSON(file string, data interface{}) {
	out, err := os.Create(file)
	if err != nil {
		log.Fatalf("Ошибка создания JSON файла: %v", err)
	}
	defer out.Close()

	var encoded []byte
	if *prettyFlag {
		encoded, err = json.MarshalIndent(data, "", "  ")
	} else {
		encoded, err = json.Marshal(data)
	}
	if err != nil {
		log.Fatalf("Ошибка кодирования JSON: %v", err)
	}
	out.Write(encoded)
	fmt.Printf("💾 JSON сохранен: %s\n", absPath(file))
}

func buildFileInfo(path string, info os.FileInfo) *FileInfo {
	size := getSize(path)
	totalSize += size

	md5sum := ""
	if !info.IsDir() {
		md5sum = calcMD5(path)
	}

	fileType := detectType(path)
	created := info.ModTime()
	updated := info.ModTime()

	return &FileInfo{
		IsDir:        info.IsDir(),
		FullName:     info.Name(),
		Ext:          strings.ToLower(filepath.Ext(info.Name())),
		NameOnly:     strings.TrimSuffix(info.Name(), filepath.Ext(info.Name())),
		SizeBytes:    size,
		SizeHuman:    humanSize(size),
		FullPath:     path,
		FullPathOrig: path,
		Created:      created,
		Updated:      updated,
		Perm:         info.Mode().String(),
		Md5:          md5sum,
		FileType:     fileType,
	}
}

func updateProgress() {
	mutex.Lock()
	defer mutex.Unlock()
	count++
	if count >= lastReport {
		elapsed := time.Since(startTime).Seconds()
		speed := float64(count) / elapsed
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Printf("📊 %d файлов | %s прошло | %.2f MB | скорость %.1f ф/с\n",
			count, time.Since(startTime).Truncate(time.Second), float64(m.Alloc)/1024/1024, speed)
		logger.Printf("%d файлов, %.1f MB, %.1f ф/с\n", count, float64(m.Alloc)/1024/1024, speed)

		switch {
		case count < 1000:
			lastReport += 100
		case count < 10000:
			lastReport += 1000
		default:
			lastReport += 10000
		}
	}
}

func shouldExclude(path string) bool {
	for _, ex := range excluded {
		if ex != "" && strings.Contains(path, ex) {
			return true
		}
	}
	return false
}

func getSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	filepath.Walk(path, func(_ string, inf os.FileInfo, err error) error {
		if err == nil && !inf.IsDir() {
			total += inf.Size()
		}
		return nil
	})
	return total
}

func calcMD5(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := md5.New()
	_, _ = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

func detectType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
		return "image"
	case ".mp4", ".avi", ".mkv", ".mov":
		return "video"
	case ".mp3", ".wav", ".flac":
		return "audio"
	case ".txt", ".md", ".log":
		return "text"
	case ".go", ".js", ".py", ".sh", ".json", ".yaml", ".yml":
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
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}

func absPath(path string) string {
	p, _ := filepath.Abs(path)
	return p
}

func initLog() {
	logFileName := strings.TrimSuffix(*outputFlag, ".json") + ".log"
	var err error
	logFile, err = os.Create(logFileName)
	if err != nil {
		log.Fatalf("Ошибка создания лог-файла: %v", err)
	}
	logger = log.New(logFile, "", log.LstdFlags)
	fmt.Printf("🧾 Лог файл: %s\n", absPath(logFileName))
}
