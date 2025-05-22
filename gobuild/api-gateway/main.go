package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"gobuild/api-gateway/auth"
	"gobuild/api-gateway/users"
	"gobuild/shared/kafka"
	"gobuild/shared/message"
)

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"user"`
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

	buildOrchestratorURL := os.Getenv("BUILD_ORCHESTRATOR_URL")
	if buildOrchestratorURL == "" {
		buildOrchestratorURL = "http://build-orchestrator:8082"
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})
	defer redisClient.Close()

	userStore := users.NewUserStore(redisClient)

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

	r.HandleFunc("/api/register", func(w http.ResponseWriter, r *http.Request) {
		var regReq RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&regReq); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if regReq.Email == "" || regReq.Password == "" {
			http.Error(w, "Email and password are required", http.StatusBadRequest)
			return
		}

		user := &users.User{
			ID:        uuid.New().String(),
			Email:     regReq.Email,
			Password:  regReq.Password,
			Role:      "user",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		err := userStore.Create(context.Background(), user)
		if err != nil {
			if err == users.ErrUserAlreadyExists {
				http.Error(w, "User with this email already exists", http.StatusConflict)
				return
			}
			log.Printf("Failed to create user: %v", err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}

		token, err := auth.GenerateToken(user.ID, user.Email, user.Role)
		if err != nil {
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}

		resp := LoginResponse{
			Token: token,
		}
		resp.User.ID = user.ID
		resp.User.Email = user.Email
		resp.User.Role = user.Role

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}).Methods("POST")

	r.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		var loginReq LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&loginReq); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if loginReq.Email == "" || loginReq.Password == "" {
			http.Error(w, "Email and password are required", http.StatusBadRequest)
			return
		}

		user, err := userStore.Authenticate(context.Background(), loginReq.Email, loginReq.Password)
		if err != nil {
			if err == users.ErrUserNotFound || err == users.ErrInvalidCredentials {
				http.Error(w, "Invalid email or password", http.StatusUnauthorized)
				return
			}
			log.Printf("Authentication error: %v", err)
			http.Error(w, "Authentication failed", http.StatusInternalServerError)
			return
		}

		token, err := auth.GenerateToken(user.ID, user.Email, user.Role)
		if err != nil {
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}

		resp := LoginResponse{
			Token: token,
		}
		resp.User.ID = user.ID
		resp.User.Email = user.Email
		resp.User.Role = user.Role

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
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

		url := fmt.Sprintf("%s/api/builds/%s", buildOrchestratorURL, buildID)

		client := &http.Client{
			Timeout: 5 * time.Second,
		}

		resp, err := client.Get(url)
		if err != nil {
			log.Printf("Failed to fetch build status from orchestrator: %v", err)
			http.Error(w, "Failed to fetch build status", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			http.Error(w, "Build not found", http.StatusNotFound)
			return
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			http.Error(w, string(body), resp.StatusCode)
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Failed to read response body: %v", err)
			http.Error(w, "Failed to read build status", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}).Methods("GET")

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("API Gateway Service is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
