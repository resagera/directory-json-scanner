
Хочешь, чтобы я добавил к этому ещё **подсчёт статистики** (сколько файлов каждого типа найдено) и возможность `--export` для сохранения результата в отдельный JSON?

Да

Отлично 😎
Тогда давай добавим полноценный **режим поиска с экспортом и статистикой** — как в CLI, так и в API.
Это позволит использовать инструмент как мини-“поисковик по дереву файлов” прямо по JSON-дампу.

---

## 🔍 Что будет добавлено

* Поддержка:

    * `--search` (CLI-режим)
    * `--export=<file.json>` — сохранение результатов поиска в JSON
    * `--stats` — показать статистику по типам файлов (image, code, video и т.д.)
* API `/api/search` возвращает:

  ```json
  {
    "results": [...],
    "stats": {"image":123, "code":22, ...},
    "total": 145
  }
  ```
* Возможность ограничивать `limit`, `offset`, `recursive`, `type`, `query`, `size.gt`, `size.lt`, `modified.gt`, `created.lt`.

---

## 🧩 `internal/domain/service/search.go`

*(дополним предыдущую версию)*

```go
package service

import (
	"fsjson/internal/domain/model"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SearchParams — параметры поиска
type SearchParams struct {
	Query      string
	Path       string
	Type       string
	SizeCmp    map[string]int64
	Created    map[string]time.Time
	Modified   map[string]time.Time
	Recursive  bool
	Limit      int
	Offset     int
}

// SearchResult — результат поиска
type SearchResult struct {
	FullPathOrig string    `json:"FullPathOrig"`
	SizeBytes    int64     `json:"SizeBytes"`
	FileType     string    `json:"FileType"`
	Modified     time.Time `json:"Modified"`
	Created      time.Time `json:"Created"`
}

// SearchStats — статистика найденных файлов по типам
type SearchStats map[string]int

// SearchResponse — итоговый ответ для API/CLI
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Stats   SearchStats    `json:"stats"`
	Total   int            `json:"total"`
}

// SearchFiles — основной алгоритм поиска
func SearchFiles(root *model.FileInfo, params SearchParams) SearchResponse {
	results := []SearchResult{}
	var regex *regexp.Regexp

	if params.Query != "" {
		regex = wildcardToRegex(params.Query)
	}
	startPath := strings.TrimSuffix(params.Path, string(filepath.Separator))

	var walk func(node *model.FileInfo)
	walk = func(node *model.FileInfo) {
		if startPath != "" && !strings.HasPrefix(node.FullPath, startPath) {
			return
		}

		if matchNode(node, params, regex) {
			results = append(results, SearchResult{
				FullPathOrig: node.FullPathOrig,
				SizeBytes:    node.SizeBytes,
				FileType:     node.FileType,
				Modified:     node.Updated,
				Created:      node.Created,
			})
		}

		if node.IsDir && params.Recursive {
			for i := range node.Children {
				walk(&node.Children[i])
			}
		}
	}

	walk(root)

	// пагинация
	start := params.Offset
	if start > len(results) {
		return SearchResponse{Results: []SearchResult{}, Stats: SearchStats{}, Total: 0}
	}
	end := len(results)
	if params.Limit > 0 && start+params.Limit < end {
		end = start + params.Limit
	}
	results = results[start:end]

	// статистика по типам
	stats := make(SearchStats)
	for _, r := range results {
		stats[r.FileType]++
	}

	return SearchResponse{
		Results: results,
		Stats:   stats,
		Total:   len(results),
	}
}
```

---

## 🧮 CLI: `cmd/fsjson/main.go`

Добавим флаги:

```go
searchFlag = flag.Bool("search", false, "Поиск по JSON-файлу (--file=...)")
exportFlag = flag.String("export", "", "Экспорт результатов поиска в JSON")
statsFlag  = flag.Bool("stats", false, "Показать статистику по типам файлов")
```

И обработчик после парсинга:

```go
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

	params := service.SearchParams{
		Query:     getFlag("query"),
		Path:      getFlag("path"),
		Type:      getFlag("type"),
		Recursive: true,
		Limit:     parseInt(getFlag("limit"), 100),
		Offset:    parseInt(getFlag("offset"), 0),
		SizeCmp:   parseSizeFlags(),
	}

	resp := service.SearchFiles(&root, params)

	// --- вывод в консоль ---
	for _, r := range resp.Results {
		fmt.Printf("%-10s | %10d B | %s\n", r.FileType, r.SizeBytes, r.FullPathOrig)
	}
	fmt.Printf("🔍 Найдено: %d файлов\n", resp.Total)

	// --- статистика ---
	if *statsFlag {
		fmt.Println("\n📊 Статистика по типам:")
		for t, c := range resp.Stats {
			fmt.Printf("  %-10s %d\n", t, c)
		}
	}

	// --- экспорт ---
	if *exportFlag != "" {
		b, _ := json.MarshalIndent(resp.Results, "", "  ")
		os.WriteFile(*exportFlag, b, 0644)
		fmt.Printf("💾 Результаты сохранены в %s\n", *exportFlag)
	}

	return
}
```

вспомогательные функции:

```go
func getFlag(name string) string {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--"+name+"=") {
			return strings.SplitN(arg, "=", 2)[1]
		}
	}
	return ""
}

func parseSizeFlags() map[string]int64 {
	m := make(map[string]int64)
	for _, op := range []string{"gt", "gte", "lt", "lte", "eq"} {
		val := getFlag("size." + op)
		if val == "" {
			continue
		}
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			m[op] = n
		}
	}
	return m
}
```

---

## 🌐 API: `GET /api/search`

в `startWebServer` добавь:

```go
http.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := service.SearchParams{
		Query:     q.Get("query"),
		Path:      q.Get("path"),
		Type:      q.Get("type"),
		Recursive: q.Get("recursive") != "false",
		Limit:     parseInt(q.Get("limit"), 100),
		Offset:    parseInt(q.Get("offset"), 0),
		SizeCmp:   parseSizeFilters(q),
	}
	result := service.SearchFiles(&root, params)
	writeJSON(w, result)
})
```

---

## 🧪 Примеры

### CLI:

```bash
./build --file=data.json --search --query="*.jpg" --type=image --size.gt=20000 --stats --export=found.json
```

Результат:

```
image      |     25123 B | /photos/summer/IMG_0001.jpg
image      |     34910 B | /photos/winter/IMG_1223.jpg
...
🔍 Найдено: 12 файлов

📊 Статистика по типам:
  image       12
💾 Результаты сохранены в found.json
```

---

### API:

```
GET /api/search?query=*.mp3&type=audio&size.gt=5000000&limit=5
```

Ответ:

```json
{
  "results": [
    {"FullPathOrig":"/music/rock/song1.mp3","SizeBytes":7534812,"FileType":"audio"},
    {"FullPathOrig":"/music/rock/song2.mp3","SizeBytes":6123341,"FileType":"audio"}
  ],
  "stats": {"audio":2},
  "total":2
}
```

---

Хочешь, чтобы я добавил к этому фильтры по **дате** (`modified.gt`, `created.lt`) с разбором ISO-строк (`2025-11-08T10:00:00`)?
Тогда можно будет искать «все файлы, изменённые за последние 7 дней».


Реализуй getEnvOrFlag в config

Отлично 💪
Добавим универсальную функцию `getEnvOrFlag` в новый пакет `internal/config`,
чтобы можно было **удобно получать значения параметров** —
из CLI-флагов (`--query=...`) **или** из переменных окружения (`QUERY=...`).

---

## 📁 `internal/config/config.go`

```go
package config

import (
	"flag"
	"os"
	"strings"
)

// GetEnvOrFlag возвращает значение из:
//   1️⃣ CLI флага (--name=...)
//   2️⃣ переменной окружения (NAME или FSJSON_NAME)
//   3️⃣ дефолтного значения из flag.Var, если задано
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
```

---

## ✅ Пример использования в `main.go`

```go
import "fsjson/internal/config"

// ...
params := service.SearchParams{
	Query:     config.GetEnvOrFlag("query"),
	Path:      config.GetEnvOrFlag("path"),
	Type:      config.GetEnvOrFlag("type"),
	Recursive: !config.GetBoolFlagOrEnv("nonrecursive"),
	Limit:     parseInt(config.GetEnvOrFlag("limit"), 100),
	Offset:    parseInt(config.GetEnvOrFlag("offset"), 0),
}
```

---

## 🧪 Примеры работы

### CLI-параметры:

```bash
./build --query="*.jpg" --limit=5
```

➡️ `GetEnvOrFlag("query") == "*.jpg"`

---

### Переменные окружения:

```bash
export QUERY="*.mp4"
./build --search
```

➡️ `GetEnvOrFlag("query") == "*.mp4"`

---

### Альтернативное пространство имён:

```bash
export FSJSON_TYPE=image
```

➡️ `GetEnvOrFlag("type") == "image"`

---

Хочешь, чтобы я добавил сюда ещё универсальный парсер числовых флагов
(`GetIntEnvOrFlag`) и временных значений (`GetTimeEnvOrFlag`, например `MODIFIED_GT=2025-11-01`)?


добавь фильтры по дате (modified.gt, created.lt) с разбором ISO-строк (2025-11-08T10:00:00)

Отлично 👍
Добавим полноценную поддержку **фильтров по дате** (`created` и `modified`) в поиске.
Теперь можно будет искать файлы, **созданные или изменённые до/после определённого времени**,
используя ISO-8601-строки — например:

```
--modified.gt=2025-11-01T00:00:00
--created.lt=2025-10-01
```

Работает и через **CLI**, и через **API** (`/api/search?...`).
Формат ISO поддерживает как полную дату-время, так и просто дату.

---

## 🧩 Обновлённый `internal/domain/service/search.go`

Вот полная версия с поддержкой фильтров по дате:

```go
package service

import (
	"fsjson/internal/domain/model"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SearchParams — параметры фильтрации
type SearchParams struct {
	Query      string
	Path       string
	Type       string
	SizeCmp    map[string]int64
	Created    map[string]time.Time // gt, lt, gte, lte
	Modified   map[string]time.Time // gt, lt, gte, lte
	Recursive  bool
	Limit      int
	Offset     int
}

// SearchResult — один элемент результата
type SearchResult struct {
	FullPathOrig string    `json:"FullPathOrig"`
	SizeBytes    int64     `json:"SizeBytes"`
	FileType     string    `json:"FileType"`
	Modified     time.Time `json:"Modified"`
	Created      time.Time `json:"Created"`
}

// SearchStats — статистика по типам
type SearchStats map[string]int

// SearchResponse — итоговый ответ
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Stats   SearchStats    `json:"stats"`
	Total   int            `json:"total"`
}

// SearchFiles выполняет поиск по дереву
func SearchFiles(root *model.FileInfo, params SearchParams) SearchResponse {
	results := []SearchResult{}
	var regex *regexp.Regexp

	if params.Query != "" {
		regex = wildcardToRegex(params.Query)
	}
	startPath := strings.TrimSuffix(params.Path, string(filepath.Separator))

	var walk func(node *model.FileInfo)
	walk = func(node *model.FileInfo) {
		if startPath != "" && !strings.HasPrefix(node.FullPath, startPath) {
			return
		}

		if matchNode(node, params, regex) {
			results = append(results, SearchResult{
				FullPathOrig: node.FullPathOrig,
				SizeBytes:    node.SizeBytes,
				FileType:     node.FileType,
				Modified:     node.Updated,
				Created:      node.Created,
			})
		}

		if node.IsDir && params.Recursive {
			for i := range node.Children {
				walk(&node.Children[i])
			}
		}
	}

	walk(root)

	start := params.Offset
	if start > len(results) {
		return SearchResponse{Results: []SearchResult{}, Stats: SearchStats{}, Total: 0}
	}
	end := len(results)
	if params.Limit > 0 && start+params.Limit < end {
		end = start + params.Limit
	}
	results = results[start:end]

	stats := make(SearchStats)
	for _, r := range results {
		stats[r.FileType]++
	}

	return SearchResponse{
		Results: results,
		Stats:   stats,
		Total:   len(results),
	}
}

// matchNode — фильтрация узла по параметрам
func matchNode(n *model.FileInfo, p SearchParams, re *regexp.Regexp) bool {
	// query
	if re != nil && !re.MatchString(strings.ToLower(n.FullName)) {
		return false
	}

	// type
	if p.Type != "" && n.FileType != p.Type {
		return false
	}

	// size
	for op, val := range p.SizeCmp {
		switch op {
		case "gt":
			if !(n.SizeBytes > val) {
				return false
			}
		case "gte":
			if !(n.SizeBytes >= val) {
				return false
			}
		case "lt":
			if !(n.SizeBytes < val) {
				return false
			}
		case "lte":
			if !(n.SizeBytes <= val) {
				return false
			}
		case "eq":
			if n.SizeBytes != val {
				return false
			}
		}
	}

	// created
	for op, t := range p.Created {
		switch op {
		case "gt":
			if !n.Created.After(t) {
				return false
			}
		case "gte":
			if n.Created.Before(t) {
				return false
			}
		case "lt":
			if !n.Created.Before(t) {
				return false
			}
		case "lte":
			if n.Created.After(t) {
				return false
			}
		}
	}

	// modified
	for op, t := range p.Modified {
		switch op {
		case "gt":
			if !n.Updated.After(t) {
				return false
			}
		case "gte":
			if n.Updated.Before(t) {
				return false
			}
		case "lt":
			if !n.Updated.Before(t) {
				return false
			}
		case "lte":
			if n.Updated.After(t) {
				return false
			}
		}
	}

	return true
}

func wildcardToRegex(q string) *regexp.Regexp {
	q = strings.ToLower(q)
	q = strings.ReplaceAll(q, ".", "\\.")
	q = strings.ReplaceAll(q, "*", ".*")
	q = strings.ReplaceAll(q, "?", ".")
	re := regexp.MustCompile(q)
	return re
}
```

---

## 🧮 Разбор ISO-даты

Добавим универсальный парсер:

📁 `internal/config/timeparse.go`:

```go
package config

import (
	"strings"
	"time"
)

// ParseISOTime поддерживает форматы:
//   2025-11-08
//   2025-11-08T10:00
//   2025-11-08T10:00:00
//   2025-11-08T10:00:00Z
func ParseISOTime(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
	}
	s = strings.TrimSpace(s)
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, time.ErrFormat
}
```

---

## ⚙️ В `main.go` или в API

Добавим парсинг флагов `--modified.gt`, `--created.lt` и т.д.:

```go
func parseTimeFilters(prefix string) map[string]time.Time {
	m := make(map[string]time.Time)
	for _, op := range []string{"gt", "gte", "lt", "lte"} {
		key := prefix + "." + op
		val := getFlag(key)
		if val == "" {
			continue
		}
		if t, err := config.ParseISOTime(val); err == nil {
			m[op] = t
		}
	}
	return m
}
```

Использование:

```go
params := service.SearchParams{
	Query:     config.GetEnvOrFlag("query"),
	Path:      config.GetEnvOrFlag("path"),
	Type:      config.GetEnvOrFlag("type"),
	Recursive: true,
	Limit:     parseInt(config.GetEnvOrFlag("limit"), 100),
	Offset:    parseInt(config.GetEnvOrFlag("offset"), 0),
	SizeCmp:   parseSizeFlags(),
	Created:   parseTimeFilters("created"),
	Modified:  parseTimeFilters("modified"),
}
```

---

## 🌐 API `/api/search`

Парсер для даты из query:

```go
func parseTimeFiltersFromQuery(q url.Values, prefix string) map[string]time.Time {
	m := make(map[string]time.Time)
	for _, op := range []string{"gt", "gte", "lt", "lte"} {
		key := prefix + "." + op
		if v := q.Get(key); v != "" {
			if t, err := config.ParseISOTime(v); err == nil {
				m[op] = t
			}
		}
	}
	return m
}
```

и при сборке `SearchParams`:

```go
params := service.SearchParams{
	Query:     q.Get("query"),
	Path:      q.Get("path"),
	Type:      q.Get("type"),
	Recursive: q.Get("recursive") != "false",
	Limit:     parseInt(q.Get("limit"), 100),
	Offset:    parseInt(q.Get("offset"), 0),
	SizeCmp:   parseSizeFilters(q),
	Created:   parseTimeFiltersFromQuery(q, "created"),
	Modified:  parseTimeFiltersFromQuery(q, "modified"),
}
```

---

## 🧪 Примеры использования

### CLI:

```bash
./build --file=data.json --search \
  --query="*.jpg" \
  --created.gt=2025-01-01 \
  --modified.lt=2025-11-01T00:00:00 \
  --type=image --limit=5
```

➡️ Покажет все картинки, созданные в 2025 году и не изменённые после 1 ноября.

---

### API:

```
GET /api/search?query=*.go&type=code&modified.gt=2025-11-01T00:00:00Z
```

Ответ:

```json
{
  "results": [
    {
      "FullPathOrig": "/projects/app/main.go",
      "SizeBytes": 4213,
      "FileType": "code",
      "Modified": "2025-11-03T09:42:12Z"
    }
  ],
  "stats": {"code":1},
  "total": 1
}
```

---

Хочешь, чтобы я добавил сюда также возможность фильтровать по **множеству типов** (например, `type=image,video`) и **по диапазону размеров** (`--size.between=100000,5000000`)?


Да, добавь возможность фильтровать по множеству типов (например, type=image,video) и по диапазону размеров (--size.between=100000,5000000)

Отлично 🔥
Добавим поддержку сразу двух мощных фич в поиске:

1. **Фильтр по множеству типов** — `--type=image,video` или `type=image,code,other`
2. **Диапазон размеров** — `--size.between=100000,5000000`

Эти фильтры будут работать как в **CLI**, так и в **API** (`/api/search?...`).
Полностью совместимы с уже существующими `gt`, `lt`, `eq`, `modified`, `created` и т.д.

---

## 🧩 Обновлённый `internal/domain/service/search.go`

Вот финальная версия поиска (универсальная и самодостаточная):

```go
package service

import (
	"fsjson/internal/domain/model"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SearchParams — параметры фильтрации
type SearchParams struct {
	Query      string
	Path       string
	Types      []string
	SizeCmp    map[string]int64
	Created    map[string]time.Time
	Modified   map[string]time.Time
	Recursive  bool
	Limit      int
	Offset     int
}

// SearchResult — один элемент результата
type SearchResult struct {
	FullPathOrig string    `json:"FullPathOrig"`
	SizeBytes    int64     `json:"SizeBytes"`
	FileType     string    `json:"FileType"`
	Modified     time.Time `json:"Modified"`
	Created      time.Time `json:"Created"`
}

// SearchStats — статистика по типам
type SearchStats map[string]int

// SearchResponse — итоговый ответ
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Stats   SearchStats    `json:"stats"`
	Total   int            `json:"total"`
}

// SearchFiles — основной алгоритм поиска
func SearchFiles(root *model.FileInfo, params SearchParams) SearchResponse {
	results := []SearchResult{}
	var regex *regexp.Regexp

	if params.Query != "" {
		regex = wildcardToRegex(params.Query)
	}

	startPath := strings.TrimSuffix(params.Path, string(filepath.Separator))

	typeSet := make(map[string]bool)
	for _, t := range params.Types {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			typeSet[t] = true
		}
	}

	var walk func(node *model.FileInfo)
	walk = func(node *model.FileInfo) {
		if startPath != "" && !strings.HasPrefix(node.FullPath, startPath) {
			return
		}

		if matchNode(node, params, regex, typeSet) {
			results = append(results, SearchResult{
				FullPathOrig: node.FullPathOrig,
				SizeBytes:    node.SizeBytes,
				FileType:     node.FileType,
				Modified:     node.Updated,
				Created:      node.Created,
			})
		}

		if node.IsDir && params.Recursive {
			for i := range node.Children {
				walk(&node.Children[i])
			}
		}
	}

	walk(root)

	// пагинация
	start := params.Offset
	if start > len(results) {
		return SearchResponse{Results: []SearchResult{}, Stats: SearchStats{}, Total: 0}
	}
	end := len(results)
	if params.Limit > 0 && start+params.Limit < end {
		end = start + params.Limit
	}
	results = results[start:end]

	stats := make(SearchStats)
	for _, r := range results {
		stats[r.FileType]++
	}

	return SearchResponse{
		Results: results,
		Stats:   stats,
		Total:   len(results),
	}
}

// matchNode — фильтрация узла по всем параметрам
func matchNode(n *model.FileInfo, p SearchParams, re *regexp.Regexp, typeSet map[string]bool) bool {
	// query
	if re != nil && !re.MatchString(strings.ToLower(n.FullName)) {
		return false
	}

	// type (множественный)
	if len(typeSet) > 0 && !typeSet[strings.ToLower(n.FileType)] {
		return false
	}

	// size (все операции включая between)
	for op, val := range p.SizeCmp {
		switch op {
		case "gt":
			if !(n.SizeBytes > val) {
				return false
			}
		case "gte":
			if !(n.SizeBytes >= val) {
				return false
			}
		case "lt":
			if !(n.SizeBytes < val) {
				return false
			}
		case "lte":
			if !(n.SizeBytes <= val) {
				return false
			}
		case "eq":
			if n.SizeBytes != val {
				return false
			}
		case "between":
			// диапазон задан как min,max
			min := p.SizeCmp["between_min"]
			max := p.SizeCmp["between_max"]
			if !(n.SizeBytes >= min && n.SizeBytes <= max) {
				return false
			}
		}
	}

	// created
	for op, t := range p.Created {
		switch op {
		case "gt":
			if !n.Created.After(t) {
				return false
			}
		case "gte":
			if n.Created.Before(t) {
				return false
			}
		case "lt":
			if !n.Created.Before(t) {
				return false
			}
		case "lte":
			if n.Created.After(t) {
				return false
			}
		}
	}

	// modified
	for op, t := range p.Modified {
		switch op {
		case "gt":
			if !n.Updated.After(t) {
				return false
			}
		case "gte":
			if n.Updated.Before(t) {
				return false
			}
		case "lt":
			if !n.Updated.Before(t) {
				return false
			}
		case "lte":
			if n.Updated.After(t) {
				return false
			}
		}
	}

	return true
}

// wildcardToRegex — поддержка шаблонов (*, ?)
func wildcardToRegex(q string) *regexp.Regexp {
	q = strings.ToLower(q)
	q = strings.ReplaceAll(q, ".", "\\.")
	q = strings.ReplaceAll(q, "*", ".*")
	q = strings.ReplaceAll(q, "?", ".")
	re := regexp.MustCompile(q)
	return re
}
```

---

## ⚙️ Обновим парсер флагов и запросов

### CLI и API

Добавим новый парсер диапазонов и множественных типов.

📁 `internal/config/parse_filters.go`

```go
package config

import (
	"net/url"
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
func ParseTimeFilters(m url.Values, prefix string) map[string]time.Time {
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
```

---

## 🌐 Пример запроса API

```
GET /api/search?query=*.mp4&type=image,video&size.between=100000,100000000&created.gt=2025-01-01
```

Ответ:

```json
{
  "results": [
    {
      "FullPathOrig": "/videos/travel/001.mp4",
      "SizeBytes": 48300000,
      "FileType": "video"
    },
    {
      "FullPathOrig": "/photos/holiday/IMG_3322.jpg",
      "SizeBytes": 205000,
      "FileType": "image"
    }
  ],
  "stats": {"video":1,"image":1},
  "total": 2
}
```

---

## 💻 CLI-пример

```bash
./build --file=data.json --search \
  --query="*.go" \
  --type=code,text \
  --size.between=1000,100000 \
  --modified.gt=2025-11-01T00:00:00 \
  --limit=10
```

Выведет только `.go` и `.txt` файлы размером от 1 КБ до 100 КБ, изменённые после 1 ноября 2025.

---

Хочешь, чтобы я добавил **ещё один параметр сортировки** — например, `--sort=size.desc` или `--sort=created.asc`?
Это позволит выводить результаты поиска в упорядоченном виде (по размеру, дате, имени и т.п.).


добавь чтобы при использовании неуказанных параметров в консоли, приложение не останавливалось, сейчас мне просто выводит список существующих параметров и ошибку о несуществующих параметрах

Отлично 💡
Ты наткнулся на типичное поведение стандартного пакета Go `flag`:
если в аргументах есть **неизвестный флаг**, программа завершается с ошибкой:

```
flag provided but not defined: -unknown
Usage of ./build:
  -dir ...
  -output ...
  ...
```

Мы можем это исправить, **сделав обработку флагов более гибкой** — чтобы
неизвестные флаги **просто игнорировались**, но при этом зарегистрированные
(например `--dir`, `--output`, `--merge`) продолжали работать как прежде.

---

## ✅ Решение: «мягкий» парсер аргументов

Создадим универсальную обёртку, которая:

* парсит только **известные** флаги,
* игнорирует неизвестные,
* сохраняет поведение `flag` для help (`--help`).

---

### 📁 `internal/config/flagparser.go`

```go
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
```

---

## 🧩 Как использовать

В `main.go` просто **замени строку:**

```go
flag.Parse()
```

на:

```go
import "fsjson/internal/config"

config.ParseFlagsSafe()
```

---

## ✅ Пример

```bash
./build --dir=/data --output=result.json --foo=bar --nope --pretty
```

Теперь вывод:

```
⚠️  Игнорирую неизвестный параметр: --foo=bar
⚠️  Игнорирую неизвестный параметр: --nope
📁 Начало сканирования: /data
...
```

Программа **продолжает работать**, просто уведомляя о пропуске флагов.
Такое поведение идеально подходит для кросс-совместимости,
если ты запускаешь инструмент с внешними скриптами или через обёртки.

---

## 💡 Бонус

Можно включить «строгий режим» с переменной окружения:

```bash
FSJSON_STRICT_FLAGS=true
```

и в `ParseFlagsSafe` добавить:

```go
if os.Getenv("FSJSON_STRICT_FLAGS") == "true" && !knownFlags[arg] {
    fmt.Fprintf(os.Stderr, "❌ Неизвестный флаг: %s\n", arg)
    os.Exit(1)
}
```

---

Хочешь, чтобы я добавил туда **автоматическую подсказку похожего флага** (например, `--ouput` → `--output`)?

