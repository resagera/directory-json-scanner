Я пишу апи для поиска файла в директории. Какие опции мне нужно реализовать? (может есть какая-то принятая практика для этого)

---

Отличный вопрос! Для API поиска файлов есть устоявшаяся практика, основанная на опыте существующих систем. Вот набор опций, который стоит реализовать, от самых базовых до продвинутых.

### Базовые (обязательные) опции

1.  **`q` или `query`** (строка): Сама поисковая строка. Это может быть просто текст или поддержка простых шаблонов (wildcards).
    *   *Пример:* `GET /api/search?q=report.pdf`

2.  **`path`** (строка): Корневая директория для поиска. Если не указана, поиск ведётся с корневой директории пользователя или по умолчанию.
    *   *Пример:* `GET /api/search?q=*.txt&path=/home/user/documents`

### Расширенные критерии поиска

3.  **`type`** (enum: `file`, `directory`): Фильтр по типу объекта.
    *   *Пример:* `GET /api/search?type=directory&q=temp`

4.  **`size`** (объект или строка): Фильтр по размеру. Удобно использовать операторы: `gt` (greater than), `gte`, `lt`, `lte`, `eq`.
    *   *Пример (объект в JSON body):* `"size": {"gte": 1024, "lt": 1048576}` // от 1КБ до 1МБ
    *   *Пример (в query string):* `GET /api/search?size=gt:1024&size=lt:1048576`

5.  **`modified` / `created`** (объект или строка): Фильтр по дате изменения/создания. Аналогично размеру, используем операторы с датами в ISO8601 или Unix timestamp.
    *   *Пример:* `"modified": {"gte": "2023-01-01T00:00:00Z"}`

6.  **`recursive`** (boolean): Осуществлять ли поиск во вложенных директориях. По умолчанию `true`.
    *   *Пример:* `GET /api/search?q=*.log&recursive=false` // только в текущей директории

### Управление результатами (Пагинация и Сортировка)

7.  **`limit`** (number): Максимальное количество результатов в ответе.
    *   *Пример:* `GET /api/search?q=*&limit=50`

8.  **`offset`** или `skip` (number): Количество результатов to skip (для пагинации).
    *   *Пример:* `GET /api/search?q=*&limit=50&offset=100` // третья страница

9.  **`sort`** (string): Поле и направление сортировки.
    *   *Пример:* `GET /api/search?q=*&sort=name` // по имени asc
    *   *Пример:* `GET /api/search?q=*&sort=-modified` // по дате изменения desc

### Дополнительные полезные опции

10. **`case_sensitive`** (boolean): Учитывать ли регистр. По умолчанию `false`.
    *   *Пример:* `GET /api/search?q=ReadMe&case_sensitive=true`

11. **`include_hidden`** (boolean): Включать ли скрытые файлы/папки (начинающиеся с `.` в Linux/Mac). По умолчанию `false`.

12. **`fields`** (массив строк): Какие поля включать в ответ (projection). Позволяет сэкономить трафик.
    *   *Пример:* `GET /api/search?q=*.jpg&fields=name,size,path`

13. **`mime_type`** (string): Фильтр по MIME-типу.
    *   *Пример:* `GET /api/search?mime_type=image/png`

---

### Пример реализации на Node.js (Express)

```javascript
app.get('/api/search', (req, res) => {
  const {
    q, // поисковый запрос
    path = '/', // путь по умолчанию
    type, // 'file' или 'directory'
    size, // строка типа "gt:1024"
    modified, // строка типа "lt:2023-01-01"
    recursive = 'true', // строки, т.к. из query
    limit = 100,
    offset = 0,
    sort = 'name',
    case_sensitive = 'false',
    include_hidden = 'false'
  } = req.query;

  // 1. Построение объекта-фильтра для вашей файловой системы
  const filter = {};

  if (q) {
    filter.name = {
      contains: q,
      caseSensitive: case_sensitive === 'true'
    };
  }

  if (type) {
    filter.type = type;
  }

  if (size) {
    // Парсинг строки "gt:1024"
    const [operator, value] = size.split(':');
    filter.size = { [operator]: parseInt(value) };
  }

  // ... аналогично для modified, created

  // 2. Опции для поиска
  const options = {
    path,
    recursive: recursive === 'true',
    includeHidden: include_hidden === 'true',
    limit: parseInt(limit),
    offset: parseInt(offset),
    sort
  };

  // 3. Вызов вашего сервиса поиска файлов
  const searchResult = await fileSearchService.search(filter, options);

  // 4. Форматирование ответа (желательно с пагинацией)
  res.json({
    data: searchResult.items,
    pagination: {
      total: searchResult.total,
      limit: options.limit,
      offset: options.offset
    }
  });
});
```

---

### Рекомендации и лучшие практики

1.  **Используйте POST для сложных запросов.** Если критериев много (например, несколько условий для размера и даты), лучше использовать POST с JSON body, чтобы не городить огромную и сложную query string.
2.  **Пагинация обязательна.** Всегда ограничивайте количество результатов по умолчанию и предоставляйте механизм пагинации.
3.  **Стандартизируйте форматы дат.** Используйте ISO8601.
4.  **Единый формат ответа.** Всегда возвращайте ответ в едином формате, включая информацию о пагинации и ошибках.
5.  **Безопасность.** Валидируйте параметр `path`, чтобы пользователь не мог выйти за пределы разрешённой ему директории.
6.  **Производительность.** Для больших объёмов данных подумайте об индексации (например, с помощью `find` или `locate` в Linux, или собственной БД).

Этот набор опций покроет ~95% всех потребностей в поиске файлов и будет интуитивно понятен пользователям вашего API.

---

Реализуй поиск в json файле (указанном во флаге fileFlag в веб режиме или обычном) как для консольного вывода результатов, так и в API.
Параметры для поиска:
query - поиск по строке или подстроке в имени файла (можно и шаблоны использовать)
path - путь поиска (если не указан, то от корня)
type - тип файла (поле FileType)
size - размер файла (а так же gt (greater than), gte, lt, lte, eq. для диапазонов)
modified / created
recursive (boolean): Осуществлять ли поиск во вложенных директориях. По умолчанию true
так же для результата обработка параметров: limit и offset
результат - список полных путей файлов (или директорий) - поле FullPathOrig

---

Отлично 👍
Ты хочешь мощный поиск **по JSON-структуре файловой системы**, доступный **и через CLI**, и через **веб-API**.
Сделаем это правильно — с учётом диапазонов, типов, путей и постраничного вывода.

---

## 📁 Что будет добавлено

1. Новый пакет `internal/domain/service/search.go`
   — ядро поиска с фильтрами, типизацией и рекурсией.

2. Поддержка параметров:

   ```
   query, path, type, size, size.gt/gte/lt/lte/eq,
   created, modified,
   recursive (bool), limit, offset
   ```

3. CLI-режим:

   ```
   ./build --file=example.json --search --query="log" --type=text --size.gt=1024 --limit=10
   ```

4. Веб-API:

   ```
   GET /api/search?query=jpg&type=image&size.gt=1000000&recursive=false
   ```

5. Возвращает список `FullPathOrig` (и, при CLI, — ещё и человекочитаемое описание).

---

## 🧩 `internal/domain/service/search.go`

```go
package service

import (
	"fmt"
	"fsjson/internal/domain/model"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SearchParams — структура фильтров поиска
type SearchParams struct {
	Query      string
	Path       string
	Type       string
	SizeCmp    map[string]int64 // gt/gte/lt/lte/eq
	Created    map[string]time.Time
	Modified   map[string]time.Time
	Recursive  bool
	Limit      int
	Offset     int
}

// SearchResult — результат поиска
type SearchResult struct {
	FullPathOrig string
	SizeBytes    int64
	FileType     string
	Modified     time.Time
	Created      time.Time
}

// SearchFiles — выполняет поиск в дереве
func SearchFiles(root *model.FileInfo, params SearchParams) []SearchResult {
	results := []SearchResult{}
	var regex *regexp.Regexp

	if params.Query != "" {
		regex = wildcardToRegex(params.Query)
	}
	startPath := strings.TrimSuffix(params.Path, string(filepath.Separator))

	var walk func(node *model.FileInfo)
	walk = func(node *model.FileInfo) {
		// если задан path — ищем только в нужном подкаталоге
		if startPath != "" && !strings.HasPrefix(node.FullPath, startPath) {
			return
		}

		// фильтруем сам элемент
		if matchNode(node, params, regex) {
			results = append(results, SearchResult{
				FullPathOrig: node.FullPathOrig,
				SizeBytes:    node.SizeBytes,
				FileType:     node.FileType,
				Modified:     node.Updated,
				Created:      node.Created,
			})
		}

		// рекурсивный обход
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
		return []SearchResult{}
	}
	end := len(results)
	if params.Limit > 0 && start+params.Limit < end {
		end = start + params.Limit
	}

	return results[start:end]
}

// matchNode — проверка совпадения элемента с фильтрами
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

	// created/modified диапазоны (если заданы)
	for op, t := range p.Created {
		switch op {
		case "gt":
			if !n.Created.After(t) {
				return false
			}
		case "lt":
			if !n.Created.Before(t) {
				return false
			}
		}
	}
	for op, t := range p.Modified {
		switch op {
		case "gt":
			if !n.Updated.After(t) {
				return false
			}
		case "lt":
			if !n.Updated.Before(t) {
				return false
			}
		}
	}

	return true
}

// wildcardToRegex превращает шаблон с * и ? в regex
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

## ⚙️ CLI-режим (`cmd/fsjson/main.go`)

Добавь новый флаг:

```go
searchFlag = flag.Bool("search", false, "Поиск по JSON-файлу (--file=...)")
```

И вставь после парсинга:

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

    // разбор параметров из env/cli (упрощённо)
    params := service.SearchParams{
        Query:     getEnvOrFlag("query"),
        Path:      getEnvOrFlag("path"),
        Type:      getEnvOrFlag("type"),
        Recursive: true,
        Limit:     50,
    }

    results := service.SearchFiles(&root, params)
    for _, r := range results {
        fmt.Printf("%s (%s, %d bytes)\n", r.FullPathOrig, r.FileType, r.SizeBytes)
    }
    fmt.Printf("🔍 Найдено %d элементов\n", len(results))
    return
}
```

*(функция `getEnvOrFlag` просто берёт переменные окружения или флаги, если ты добавишь их)*

---

## 🌐 Веб-API `/api/search`

В `startWebServer` добавь:

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
    results := service.SearchFiles(&root, params)
    writeJSON(w, results)
})
```

вспомогательные функции:

```go
func parseSizeFilters(q url.Values) map[string]int64 {
    m := make(map[string]int64)
    for _, k := range []string{"gt", "gte", "lt", "lte", "eq"} {
        if v := q.Get("size." + k); v != "" {
            if n, err := strconv.ParseInt(v, 10, 64); err == nil {
                m[k] = n
            }
        }
    }
    return m
}

func parseInt(s string, def int) int {
    if s == "" {
        return def
    }
    n, err := strconv.Atoi(s)
    if err != nil {
        return def
    }
    return n
}
```

---

## 🧪 Примеры запросов

### Через CLI:

```bash
./build --file=4tbWdPortable.json --search --query="*.mp4" --type=video --size.gt=100000000
```

### Через браузер/API:

```
http://localhost:8080/api/search?query=*.jpg&type=image&size.lt=500000&recursive=true&limit=50
```

Ответ:

```json
[
  {"FullPathOrig":"/photos/2024/summer/img001.jpg","SizeBytes":34211,"FileType":"image"},
  {"FullPathOrig":"/photos/2024/summer/img002.jpg","SizeBytes":29901,"FileType":"image"}
]
```

---

Хочешь, чтобы я добавил к этому ещё **подсчёт статистики** (сколько файлов каждого типа найдено) и возможность `--export` для сохранения результата в отдельный JSON?
