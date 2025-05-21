package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type BuildStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Logs   string `json:"logs"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
	}

	// Enable CORS for frontend
	corsMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// Mock build status endpoint
	http.HandleFunc("/api/builds", func(w http.ResponseWriter, r *http.Request) {
		builds := []BuildStatus{
			{ID: "build-123", Status: "completed", Logs: "Build completed successfully"},
			{ID: "build-124", Status: "in-progress", Logs: "Installing dependencies..."},
			{ID: "build-125", Status: "failed", Logs: "Build failed: compilation error"},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(builds)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap handlers with CORS middleware
	handler := corsMiddleware(http.DefaultServeMux)

	log.Printf("Status Dashboard API is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}
