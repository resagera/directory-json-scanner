package infrastructure

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"fsjson/internal/domain/model"
	"fsjson/internal/domain/service"
)

//go:embed static/*
var StaticFS embed.FS

func StartWebServer(jsonPath string) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		log.Fatalf("Ошибка чтения %s: %v", jsonPath, err)
	}
	var root model.FileInfo
	if err := json.Unmarshal(data, &root); err != nil {
		log.Fatalf("Ошибка разбора JSON: %v", err)
	}
	fmt.Printf("🌐 Веб-интерфейс запущен: http://localhost:8080\n📄 Загружен файл: %s\n", jsonPath)

	http.HandleFunc("/api/tree", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" || path == "/" {
			writeJSON(w, root.Children)
			return
		}
		node := findNodeByPath(&root, path)
		if node == nil {
			http.Error(w, "not found", 404)
			return
		}
		writeJSON(w, node.Children)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(indexHTML))
	})

	http.Handle("/static/", http.StripPrefix("/static/",
		http.FileServer(http.FS(StaticFS))))
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

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func findNodeByPath(node *model.FileInfo, path string) *model.FileInfo {
	if node.FullPath == path {
		return node
	}
	for i := range node.Children {
		if sub := findNodeByPath(&node.Children[i], path); sub != nil {
			return sub
		}
	}
	return nil
}
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

var indexHTML = `
<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8" />
<title>File Explorer</title>
<style>
body { font-family: sans-serif; margin: 0; background: #fafafa; }
header { background: #333; color: #fff; padding: 8px 16px; font-size: 18px; }
#tree { padding: 10px 20px; font-family: monospace; }
.item { cursor: pointer; margin-left: 20px; }
.folder::before { content: "📁 "; }
.file::before { content: "📄 "; }
.info { color: #777; margin-left: 8px; font-size: 12px; }
</style>
</head>
<body>
<header>📁 File Explorer</header>
<div id="tree"></div>

<script>
async function fetchTree(path="/") {
  const res = await fetch("/api/tree?path=" + encodeURIComponent(path));
  if (!res.ok) return [];
  return await res.json();
}

async function render(path="/", container=document.getElementById("tree")) {
  const items = await fetchTree(path);
  container.innerHTML = "";
  for (const item of items) {
    const div = document.createElement("div");
    div.className = "item " + (item.IsDir ? "folder" : "file");
    div.textContent = item.FullName;
    if (item.IsDir) {
      div.onclick = () => {
        history.pushState({ path: item.FullPath }, "", "?path=" + encodeURIComponent(item.FullPath));
        load(item.FullPath);
      };
    } else {
      const info = document.createElement("span");
      info.className = "info";
      info.textContent = ` + "`" + `${item.SizeHuman} | ${item.FileType}` + "`" + `;
      div.appendChild(info);
    }
    container.appendChild(div);
  }
}

async function load(path="/") {
  const tree = document.getElementById("tree");
  await render(path, tree);
}

window.onpopstate = () => {
  const params = new URLSearchParams(location.search);
  load(params.get("path") || "/");
};

load(new URLSearchParams(location.search).get("path") || "/");
</script>
</body>
</html>
`
