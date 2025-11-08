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
