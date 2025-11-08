package main

import (
	"flag"
	"fmt"
	"log"
	"runtime"

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
