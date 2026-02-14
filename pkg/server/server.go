package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/dashboard-advisor/pkg/analyzer"
	"github.com/dashboard-advisor/pkg/fixer"
	"github.com/dashboard-advisor/web"
)

// Handler returns an http.Handler serving the web UI and API endpoints.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/analyze", handleAnalyze)
	mux.HandleFunc("POST /api/fix", handleFix)
	mux.HandleFunc("GET /", handleIndex)
	return mux
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := web.Content.ReadFile("index.html")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func handleAnalyze(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, "error reading request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}

	engine := analyzer.DefaultEngine()
	report, err := engine.AnalyzeBytes(body)
	if err != nil {
		log.Printf("analyze error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(report)
}

func handleFix(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, "error reading request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}

	engine := analyzer.DefaultEngine()
	report, err := engine.AnalyzeBytes(body)
	if err != nil {
		log.Printf("fix analysis error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	patched, fixCount, err := fixer.ApplyFixes(body, report.Findings)
	if err != nil {
		log.Printf("fix apply error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(map[string]interface{}{
		"fixCount":  fixCount,
		"dashboard": json.RawMessage(patched),
	})
}
