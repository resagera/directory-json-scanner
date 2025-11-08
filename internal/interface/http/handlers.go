package http

import (
	"encoding/json"
	"net/http"

	"fsjson/internal/domain/model"
	"fsjson/internal/domain/service"
)

// HandleDuplicates — возвращает список групп дубликатов
func HandleDuplicates(root *model.FileInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := service.FindDuplicates(root)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
