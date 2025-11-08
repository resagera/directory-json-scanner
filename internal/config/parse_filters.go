package config

import (
	"flag"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ParseTypes разбивает строку по запятым
func ParseTypes(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// ParseSizeFilters поддерживает gt, lt, eq, between
func ParseSizeFilters(m url.Values) map[string]int64 {
	out := make(map[string]int64)
	for _, op := range []string{"gt", "gte", "lt", "lte", "eq"} {
		if v := m.Get("size." + op); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				out[op] = n
			}
		}
	}
	if v := m.Get("size.between"); v != "" {
		parts := strings.Split(v, ",")
		if len(parts) == 2 {
			min, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
			max, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
			out["between"] = 1
			out["between_min"] = min
			out["between_max"] = max
		}
	}
	return out
}

// ParseTimeFilters поддерживает gt, gte, lt, lte
func ParseTimeFilters_(m url.Values, prefix string) map[string]time.Time {
	out := make(map[string]time.Time)
	for _, op := range []string{"gt", "gte", "lt", "lte"} {
		key := prefix + "." + op
		if v := m.Get(key); v != "" {
			if t, err := ParseISOTime(v); err == nil {
				out[op] = t
			}
		}
	}
	return out
}

func ParseTimeFilters(prefix string) map[string]time.Time {
	m := make(map[string]time.Time)
	for _, op := range []string{"gt", "gte", "lt", "lte"} {
		key := prefix + "." + op
		val := GetEnvOrFlag(key)
		if val == "" {
			continue
		}
		if t, err := ParseISOTime(val); err == nil {
			m[op] = t
		}
	}
	return m
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
