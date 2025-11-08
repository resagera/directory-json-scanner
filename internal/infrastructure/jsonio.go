package infrastructure

import (
	"encoding/json"
	"fmt"
	"os"

	"fsjson/internal/domain/model"
)

// WriteFinalJSONAtomic записывает дерево в файл атомарно
func WriteFinalJSONAtomic(output string, root model.FileInfo, pretty bool) {
	tmp := output + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Ошибка создания временного файла:", err)
		return
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(root); err != nil {
		fmt.Println("Ошибка записи JSON:", err)
		_ = os.Remove(tmp)
		return
	}
	_ = f.Close()
	_ = os.Rename(tmp, output)
}

// WriteFlatJSONAtomic записывает flat-массив
func WriteFlatJSONAtomic(output string, arr []model.FileInfo, pretty bool) {
	tmp := output + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Ошибка создания временного файла:", err)
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(arr); err != nil {
		fmt.Println("Ошибка записи JSON:", err)
		_ = os.Remove(tmp)
		return
	}
	_ = f.Close()
	_ = os.Rename(tmp, output)
}

// DiagnoseJSONShape выводит тип JSON (object/array)
func DiagnoseJSONShape(path string) {
	b := make([]byte, 1)
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("🔎 diagnose: не удалось открыть %s: %v\n", path, err)
		return
	}
	defer f.Close()
	for {
		_, err = f.Read(b)
		if err != nil {
			fmt.Printf("🔎 diagnose: пустой файл?\n")
			return
		}
		if b[0] != ' ' && b[0] != '\n' && b[0] != '\t' && b[0] != '\r' {
			break
		}
	}
	switch b[0] {
	case '{':
		fmt.Println("🔎 diagnose: OBJECT (дерево)")
	case '[':
		fmt.Println("🔎 diagnose: ARRAY (flat)")
	default:
		fmt.Printf("🔎 diagnose: неожиданный байт: %q\n", b[0])
	}
}
