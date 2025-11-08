Отлично 💡
Добавим новый мощный режим — **поиск дубликатов файлов по MD5**.
Он будет работать как через **CLI**, так и через **веб-интерфейс** (через `/api/duplicates`).
Функциональность легко интегрируется в текущую архитектуру `fsjson`.

---

## 🧩 Цель

Найти все файлы, у которых одинаковое значение `Md5`.
Результат должен быть сгруппирован:
каждая группа = один хеш + список путей.

---

## 📁 Новый файл: `internal/domain/service/duplicates.go`

```go
package service

import (
	"fsjson/internal/domain/model"
	"sort"
)

// DuplicateGroup — группа файлов с одинаковым MD5
type DuplicateGroup struct {
	Md5   string   `json:"md5"`
	Files []string `json:"files"`
	Count int      `json:"count"`
	Size  int64    `json:"size"`
}

// DuplicatesResponse — результат поиска дубликатов
type DuplicatesResponse struct {
	Groups []DuplicateGroup `json:"groups"`
	Total  int              `json:"total_groups"`
	Files  int              `json:"total_files"`
}

// FindDuplicates — ищет все файлы с одинаковым MD5
func FindDuplicates(root *model.FileInfo) DuplicatesResponse {
	md5map := make(map[string][]*model.FileInfo)

	var walk func(n *model.FileInfo)
	walk = func(n *model.FileInfo) {
		if n == nil {
			return
		}
		if !n.IsDir && n.Md5 != "" {
			md5map[n.Md5] = append(md5map[n.Md5], n)
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(root)

	groups := make([]DuplicateGroup, 0, len(md5map))
	totalFiles := 0
	for md5, files := range md5map {
		if len(files) > 1 { // только дубликаты
			group := DuplicateGroup{Md5: md5, Count: len(files)}
			for _, f := range files {
				group.Files = append(group.Files, f.FullPathOrig)
				group.Size += f.SizeBytes
			}
			totalFiles += len(files)
			groups = append(groups, group)
		}
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count == groups[j].Count {
			return groups[i].Size > groups[j].Size
		}
		return groups[i].Count > groups[j].Count
	})

	return DuplicatesResponse{
		Groups: groups,
		Total:  len(groups),
		Files:  totalFiles,
	}
}
```

---

## 🌐 Добавим API `/api/duplicates`

📁 `internal/interface/http/handlers.go`

```go
package http

import (
	"encoding/json"
	"fsjson/internal/domain/model"
	"fsjson/internal/domain/service"
	"net/http"
)

// HandleDuplicates — возвращает список групп дубликатов
func HandleDuplicates(root *model.FileInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := service.FindDuplicates(root)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
```

И добавляем роутер в `startWebServer`:

```go
http.HandleFunc("/api/duplicates", httpHandler.HandleDuplicates(&root))
```

---

## 💻 CLI режим

Добавим флаг `--find-duplicates`
и выведем результат в консоль при запуске без веб-интерфейса.

📁 `cmd/fsjson/main.go` (или где у тебя `main.go`):

```go
findDuplicatesFlag := flag.Bool("find-duplicates", false, "Найти дубликаты по MD5")
```

и в `main()`:

```go
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
```

---

## 🌐 Пример запроса API

```
GET /api/duplicates
```

Ответ:

```json
{
  "groups": [
    {
      "md5": "f1c9645dbc14efddc7d8a322685f26eb",
      "files": [
        "/data/docs/report1.pdf",
        "/data/backups/report1_copy.pdf"
      ],
      "count": 2,
      "size": 204800
    },
    {
      "md5": "7c4a8d09ca3762af61e59520943dc264",
      "files": [
        "/photos/2025/pic1.jpg",
        "/photos/2025/pic1_copy.jpg",
        "/photos/old/pic1_backup.jpg"
      ],
      "count": 3,
      "size": 3120000
    }
  ],
  "total_groups": 2,
  "total_files": 5
}
```

---

## 💡 CLI-пример

```bash
./build --file=scan.json --find-duplicates
```

Вывод:

```
🔍 Найдено групп дубликатов: 2, файлов-дубликатов: 5

🧩 MD5: f1c9645dbc14efddc7d8a322685f26eb (2 файлов, общий размер: 204800 байт)
   /data/docs/report1.pdf
   /data/backups/report1_copy.pdf

🧩 MD5: 7c4a8d09ca3762af61e59520943dc264 (3 файлов, общий размер: 3120000 байт)
   /photos/2025/pic1.jpg
   /photos/2025/pic1_copy.jpg
   /photos/old/pic1_backup.jpg
```

---

## 🚀 Расширения на будущее

Можно легко добавить:

* `--min-group-size=3` — показывать только группы, где ≥3 файлов.
* `--min-total-size=1048576` — только дубликаты больше 1 МБ.
* экспорт дубликатов в отдельный JSON: `--duplicates-output=dupes.json`.

---

Хочешь, чтобы я добавил **параметры фильтрации дубликатов** (по количеству файлов в группе, по минимальному размеру, по директории и т.д.)?
