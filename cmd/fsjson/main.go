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
