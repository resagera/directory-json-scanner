package app

import (
	"flag"
	"os"
	"strings"
)

// ScanConfig — параметры сканирования
type ScanConfig struct {
	RootDir string
	Exclude []string
	Output  string
	Pretty  bool
	Workers int
	SkipMD5 bool
	IOLimit int
	Resume  bool // TODO: пока не реализовано в stream-режиме
}

// MergeConfig — параметры объединения
type MergeConfig struct {
	Files         []string
	Output        string
	Pretty        bool
	Dedupe        bool
	MergeFlat     bool
	MergeChildren bool
}

// GetEnvOrFlag возвращает значение из:
//
//	1️⃣ CLI флага (--name=...)
//	2️⃣ переменной окружения (NAME или FSJSON_NAME)
//	3️⃣ дефолтного значения из flag.Var, если задано
func GetEnvOrFlag(name string) string {
	// 1️⃣ Проверяем флаг (уже спарсенные flag.*)
	f := flag.Lookup(name)
	if f != nil && f.Value.String() != f.DefValue {
		val := strings.TrimSpace(f.Value.String())
		if val != "" {
			return val
		}
	}

	// 2️⃣ Проверяем переменные окружения
	envName := strings.ToUpper(name)
	if v := os.Getenv(envName); v != "" {
		return v
	}
	if v := os.Getenv("FSJSON_" + envName); v != "" {
		return v
	}

	// 3️⃣ Возврат значения по умолчанию
	if f != nil {
		return f.DefValue
	}
	return ""
}

// GetBoolFlagOrEnv аналогичная функция, но для булевых флагов
func GetBoolFlagOrEnv(name string) bool {
	// 1️⃣ Проверка флага
	f := flag.Lookup(name)
	if f != nil && f.Value.String() != f.DefValue {
		return f.Value.String() == "true"
	}

	// 2️⃣ Переменные окружения (true/1/on)
	envName := strings.ToUpper(name)
	if v := os.Getenv(envName); v != "" {
		return parseBool(v)
	}
	if v := os.Getenv("FSJSON_" + envName); v != "" {
		return parseBool(v)
	}

	// 3️⃣ Значение по умолчанию
	if f != nil {
		return f.DefValue == "true"
	}
	return false
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}
