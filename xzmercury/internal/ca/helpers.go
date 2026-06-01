package ca

import (
	"encoding/json"
	"net/http"
)

type caErrorResponse struct {
	Error string `json:"error"`
}

func writeCAError(w http.ResponseWriter, status int, msg string) {
	writeCAJSON(w, status, caErrorResponse{Error: msg})
}

func writeCAJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
