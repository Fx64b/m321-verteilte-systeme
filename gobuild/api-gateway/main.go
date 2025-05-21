package main

import (
	"encoding/json"
	"github.com/google/uuid"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"gobuild/api-gateway/auth"
	"gobuild/shared/kafka"
	"gobuild/shared/message"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type BuildRequest struct {
	RepositoryURL string `json:"repository_url"`
	Branch        string `json:"branch"`
	CommitHash    string `json:"commit_hash"`
}

type BuildResponse struct {
	BuildID string `json:"build_id"`
	Message string `json:"message"`
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Initialize Kafka producer
	kafkaProducer, err := kafka.NewProducer("kafka:29092")
	if err != nil {
		log.Fatalf("Failed to create Kafka producer: %v", err)
	}
	defer kafkaProducer.Close()

	r := mux.NewRouter()

	r.Use(corsMiddleware)

	r.Methods("OPTIONS").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusOK)
	})

	r.Use(auth.AuthMiddleware)

	r.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		var loginReq LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&loginReq); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// TODO
		// In a real application, you would validate the credentials against a database
		// For simplicity, we're just checking if email and password are not empty
		if loginReq.Email == "" || loginReq.Password == "" {
			http.Error(w, "Email and password are required", http.StatusBadRequest)
			return
		}

		// Generate a token for the user
		token, err := auth.GenerateToken("user123", loginReq.Email, "user")
		if err != nil {
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResponse{Token: token})
	}).Methods("POST")

	r.HandleFunc("/api/builds", func(w http.ResponseWriter, r *http.Request) {
		userClaims, ok := auth.UserClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var buildReq BuildRequest
		if err := json.NewDecoder(r.Body).Decode(&buildReq); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if buildReq.RepositoryURL == "" {
			http.Error(w, "Repository URL is required", http.StatusBadRequest)
			return
		}

		buildID := uuid.New().String()
		buildMsg := message.BuildRequestMessage{
			ID:            buildID,
			RepositoryURL: buildReq.RepositoryURL,
			Branch:        buildReq.Branch,
			CommitHash:    buildReq.CommitHash,
			UserID:        userClaims.ID,
			CreatedAt:     time.Now(),
		}

		err := kafkaProducer.SendMessage("build-requests", buildID, buildMsg)
		if err != nil {
			log.Printf("Failed to send build request to Kafka: %v", err)
			http.Error(w, "Failed to process build request", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(BuildResponse{
			BuildID: buildID,
			Message: "Build request submitted successfully",
		})
	}).Methods("POST")

	r.HandleFunc("/api/builds/{buildId}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		buildID := vars["buildId"]

		// TODO
		// In a real implementation, you would retrieve the build status from a database or Redis
		// For simplicity, we're just returning a mock response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":             buildID,
			"status":         "queued",
			"repository_url": "https://github.com/example/repo",
			"created_at":     time.Now().Add(-5 * time.Minute),
		})
	}).Methods("GET")

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("API Gateway Service is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
