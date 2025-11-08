package infrastructure

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"fsjson/internal/domain/model"
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
