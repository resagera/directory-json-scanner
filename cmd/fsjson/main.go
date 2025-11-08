package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"fsjson/internal/app"
	"fsjson/internal/config"
	"fsjson/internal/domain/model"
	"fsjson/internal/domain/service"
	"fsjson/internal/infrastructure"
)

var (
	dirFlag            = flag.String("dir", ".", "Директория для сканирования")
	excludeFlag        = flag.String("exclude", "", "Исключения через запятую")
	outputFlag         = flag.String("output", "structure.json", "Выходной JSON-файл")
	prettyFlag         = flag.Bool("pretty", false, "Форматировать JSON красиво")
	streamFlag         = flag.Bool("stream", false, "Потоковая запись в temp")
	resumeFlag         = flag.Bool("resume", false, "Продолжить сканирование (только с --stream)")
	mergeFlag          = flag.String("merge", "", "Список JSON-файлов через запятую для объединения")
	workersFlag        = flag.Int("workers", runtime.NumCPU(), "Количество параллельных потоков сканирования")
	skipMd5Flag        = flag.Bool("no-md5", false, "Не вычислять MD5 для файлов")
	ioLimitFlag        = flag.Int("io-limit", 16, "Максимум одновременных I/O операций (чтение/MD5/Stat)")
	dedupeFlag         = flag.Bool("dedupe", false, "Удалять дубликаты по FullPathOrig при объединении JSON файлов")
	mergeFlatFlag      = flag.Bool("merge-flat", false, "Сохранять объединённый результат в плоском виде ([]FileInfo)")
	mergeChildrenFlag  = flag.Bool("merge-children", false, "Объединять только дочерние элементы корней")
	webFlag            = flag.Bool("web", false, "Запустить веб-интерфейс для просмотра JSON")
	fileFlag           = flag.String("file", "", "JSON-файл для просмотра в веб-интерфейсе")
	searchFlag         = flag.Bool("search", false, "Поиск по JSON-файлу (--file=...)")
	searchQuery        = flag.String("query", "", "Запрос поиска")
	searchPath         = flag.String("path", "", "Путь для поиска")
	searchTypeFile     = flag.String("type", "", "Поиск по типу")
	searchLimit        = flag.Int("limit", 100, "Поиск по типу")
	searchOffset       = flag.Int("offset", 0, "Поиск по типу")
	searchCreated      = flag.String("created", "", "Поиск по дате создания")
	searchModified     = flag.String("modified", "", "Поиск по дате изменения")
	findDuplicatesFlag = flag.Bool("find-duplicates", false, "Найти дубликаты по MD5")
)

func main() {
	config.ParseFlagsSafe()

	if *searchFlag {
		if *fileFlag == "" {
			log.Fatal("Укажите JSON-файл через --file")
		}
		data, err := os.ReadFile(*fileFlag)
		if err != nil {
			log.Fatal(err)
		}
		var root model.FileInfo
		if err := json.Unmarshal(data, &root); err != nil {
			log.Fatal(err)
		}

		// разбор параметров из env/cli (упрощённо)
		params := service.SearchParams{
			Query:     *searchQuery,
			Path:      *searchPath,
			Types:     strings.Split(*searchTypeFile, ","),
			Recursive: true,
			Limit:     *searchLimit,
			Offset:    *searchOffset,
			//SizeCmp:   parseSizeFlags(),
			Created:  config.ParseTimeFilters(*searchCreated),
			Modified: config.ParseTimeFilters(*searchModified),
		}

		results := service.SearchFiles(&root, params)
		for _, r := range results.Results {
			fmt.Printf("%s (%s, %d bytes)\n", r.FullPathOrig, r.FileType, r.SizeBytes)
		}
		fmt.Printf("🔍 Найдено %d элементов\n", results.Total)
		return
	}

	if *findDuplicatesFlag {
		data, err := os.ReadFile(*fileFlag)
		if err != nil {
			log.Fatalf("Ошибка чтения %s: %v", *fileFlag, err)
		}
		var root model.FileInfo
		if err := json.Unmarshal(data, &root); err != nil {
			log.Fatalf("Ошибка разбора JSON: %v", err)
		}

		res := service.FindDuplicates(&root)
		fmt.Printf("🔍 Найдено групп дубликатов: %d, файлов-дубликатов: %d\n\n", res.Total, res.Files)
		for _, g := range res.Groups {
			fmt.Printf("🧩 MD5: %s (%d файлов, общий размер: %d байт)\n", g.Md5, g.Count, g.Size)
			for _, f := range g.Files {
				fmt.Printf("   %s\n", f)
			}
			fmt.Println()
		}
		return
	}

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
