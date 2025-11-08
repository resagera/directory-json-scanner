package config

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// ParseFlagsSafe — безопасный парсер флагов.
// Игнорирует неизвестные параметры, но сохраняет стандартные флаги.
func ParseFlagsSafe() {
	// Если запрошена помощь — используем стандартный парсер
	for _, arg := range os.Args {
		if arg == "-h" || arg == "--help" {
			flag.Usage()
			os.Exit(0)
		}
	}

	knownFlags := make(map[string]bool)
	flag.VisitAll(func(f *flag.Flag) {
		knownFlags["--"+f.Name] = true
		knownFlags["-"+f.Name] = true
	})

	// Собираем только известные флаги и их значения
	validArgs := []string{os.Args[0]}
	skipNext := false
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]

		// пропускаем одиночные аргументы после известных флагов
		if skipNext {
			skipNext = false
			continue
		}

		// игнорируем аргументы без префикса "-"
		if !strings.HasPrefix(arg, "-") {
			validArgs = append(validArgs, arg)
			continue
		}

		// если это известный флаг (с "=" или без)
		if strings.Contains(arg, "=") {
			name := strings.SplitN(arg, "=", 2)[0]
			if knownFlags[name] {
				validArgs = append(validArgs, arg)
			}
			continue
		}

		// если флаг известный, но значение идёт через пробел
		if knownFlags[arg] {
			validArgs = append(validArgs, arg)
			// если следующий аргумент не начинается с "-", то он значение
			if i+1 < len(os.Args) && !strings.HasPrefix(os.Args[i+1], "-") {
				validArgs = append(validArgs, os.Args[i+1])
				skipNext = true
			}
			continue
		}

		// неизвестный флаг — пропускаем
		fmt.Printf("⚠️  Игнорирую неизвестный параметр: %s\n", arg)
	}

	// заменяем os.Args и парсим
	os.Args = validArgs
	flag.Parse()
}
