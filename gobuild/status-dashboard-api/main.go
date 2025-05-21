package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"gobuild/shared/kafka"
	"gobuild/shared/message"
)

type BuildStatus struct {
	ID            string    `json:"id"`
	RepositoryURL string    `json:"repository_url"`
	Branch        string    `json:"branch,omitempty"`
	CommitHash    string    `json:"commit_hash,omitempty"`
	Status        string    `json:"status"`
	Message       string    `json:"message,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	ArtifactURL   string    `json:"artifact_url,omitempty"`
	Logs          []string  `json:"logs,omitempty"`
}

type StatusDashboardAPI struct {
	redisClient *redis.Client
	builds      map[string]*BuildStatus
}

// NewStatusDashboardAPI creates a new StatusDashboardAPI
func NewStatusDashboardAPI(redisClient *redis.Client) *StatusDashboardAPI {
	return &StatusDashboardAPI{
		redisClient: redisClient,
		builds:      make(map[string]*BuildStatus),
	}
}

func (api *StatusDashboardAPI) GetBuilds(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	keys, err := api.redisClient.Keys(ctx, "build:*").Result()
	if err != nil {
		http.Error(w, "Failed to retrieve builds", http.StatusInternalServerError)
		return
	}

	builds := make([]*BuildStatus, 0, len(keys))

	for _, key := range keys {
		buildJSON, err := api.redisClient.Get(ctx, key).Result()
		if err != nil {
			log.Printf("Failed to get build %s: %v", key, err)
			continue
		}

		var build BuildStatus
		err = json.Unmarshal([]byte(buildJSON), &build)
		if err != nil {
			log.Printf("Failed to unmarshal build %s: %v", key, err)
			continue
		}

		builds = append(builds, &build)
	}

	// TODO: remove in the future
	// If no builds found in Redis, use the mock data
	if len(builds) == 0 {
		builds = append(builds,
			&BuildStatus{
				ID:            "build-123",
				RepositoryURL: "https://github.com/example/repo",
				Status:        "completed",
				Message:       "Build completed successfully",
				CreatedAt:     time.Now().Add(-30 * time.Minute),
				UpdatedAt:     time.Now().Add(-25 * time.Minute),
				ArtifactURL:   "/artifacts/build-123.tar.gz",
				Logs:          []string{"Build completed successfully"},
			},
			&BuildStatus{
				ID:            "build-124",
				RepositoryURL: "https://github.com/example/repo",
				Status:        "in-progress",
				Message:       "Installing dependencies...",
				CreatedAt:     time.Now().Add(-10 * time.Minute),
				UpdatedAt:     time.Now().Add(-5 * time.Minute),
				Logs:          []string{"Cloning repository...", "Installing dependencies..."},
			},
			&BuildStatus{
				ID:            "build-125",
				RepositoryURL: "https://github.com/example/repo",
				Status:        "failed",
				Message:       "Build failed: compilation error",
				CreatedAt:     time.Now().Add(-20 * time.Minute),
				UpdatedAt:     time.Now().Add(-18 * time.Minute),
				Logs:          []string{"Cloning repository...", "Build failed: compilation error"},
			},
		)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(builds)
}

func (api *StatusDashboardAPI) GetBuild(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	buildID := vars["buildId"]

	ctx := context.Background()

	buildJSON, err := api.redisClient.Get(ctx, "build:"+buildID).Result()
	if err != nil {
		if err == redis.Nil {
			// TODO: remove in the future
			// If build not found in Redis, check our mock data
			if buildID == "build-123" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(BuildStatus{
					ID:            "build-123",
					RepositoryURL: "https://github.com/example/repo",
					Status:        "completed",
					Message:       "Build completed successfully",
					CreatedAt:     time.Now().Add(-30 * time.Minute),
					UpdatedAt:     time.Now().Add(-25 * time.Minute),
					ArtifactURL:   "/artifacts/build-123.tar.gz",
					Logs:          []string{"Build completed successfully"},
				})
				return
			} else if buildID == "build-124" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(BuildStatus{
					ID:            "build-124",
					RepositoryURL: "https://github.com/example/repo",
					Status:        "in-progress",
					Message:       "Installing dependencies...",
					CreatedAt:     time.Now().Add(-10 * time.Minute),
					UpdatedAt:     time.Now().Add(-5 * time.Minute),
					Logs:          []string{"Cloning repository...", "Installing dependencies..."},
				})
				return
			} else if buildID == "build-125" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(BuildStatus{
					ID:            "build-125",
					RepositoryURL: "https://github.com/example/repo",
					Status:        "failed",
					Message:       "Build failed: compilation error",
					CreatedAt:     time.Now().Add(-20 * time.Minute),
					UpdatedAt:     time.Now().Add(-18 * time.Minute),
					Logs:          []string{"Cloning repository...", "Build failed: compilation error"},
				})
				return
			}
			http.Error(w, "Build not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to retrieve build", http.StatusInternalServerError)
		return
	}

	var build BuildStatus
	err = json.Unmarshal([]byte(buildJSON), &build)
	if err != nil {
		http.Error(w, "Failed to parse build data", http.StatusInternalServerError)
		return
	}

	logs, err := api.redisClient.LRange(ctx, "logs:"+buildID, 0, -1).Result()
	if err != nil && err != redis.Nil {
		log.Printf("Failed to get logs for build %s: %v", buildID, err)
	}
	build.Logs = logs

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(build)
}

// ProcessBuildStatus processes a build status update
func (api *StatusDashboardAPI) ProcessBuildStatus(statusMsg message.BuildStatusMessage) {
	ctx := context.Background()

	buildJSON, err := api.redisClient.Get(ctx, "build:"+statusMsg.BuildID).Result()
	if err != nil && err != redis.Nil {
		log.Printf("Failed to get build %s: %v", statusMsg.BuildID, err)
		return
	}

	var build BuildStatus
	if err == redis.Nil {
		// Build doesn't exist yet, create a new one
		build = BuildStatus{
			ID:        statusMsg.BuildID,
			Status:    statusMsg.Status,
			Message:   statusMsg.Message,
			CreatedAt: statusMsg.UpdatedAt,
			UpdatedAt: statusMsg.UpdatedAt,
		}
	} else {
		// Update existing build
		err = json.Unmarshal([]byte(buildJSON), &build)
		if err != nil {
			log.Printf("Failed to unmarshal build %s: %v", statusMsg.BuildID, err)
			return
		}

		build.Status = statusMsg.Status
		build.Message = statusMsg.Message
		build.UpdatedAt = statusMsg.UpdatedAt
	}

	// Save the updated build to Redis
	buildJSONBytes, err := json.Marshal(build)
	if err != nil {
		log.Printf("Failed to marshal build %s: %v", statusMsg.BuildID, err)
		return
	}
	buildJSON = string(buildJSONBytes)

	err = api.redisClient.Set(ctx, "build:"+statusMsg.BuildID, buildJSON, 24*time.Hour).Err()
	if err != nil {
		log.Printf("Failed to save build %s: %v", statusMsg.BuildID, err)
		return
	}
}

func (api *StatusDashboardAPI) ProcessBuildLog(logMsg message.BuildLogMessage) {
	ctx := context.Background()

	err := api.redisClient.RPush(ctx, "logs:"+logMsg.BuildID, logMsg.LogEntry).Err()
	if err != nil {
		log.Printf("Failed to save log entry for build %s: %v", logMsg.BuildID, err)
		return
	}

	err = api.redisClient.Expire(ctx, "logs:"+logMsg.BuildID, 24*time.Hour).Err()
	if err != nil {
		log.Printf("Failed to set expiry for logs of build %s: %v", logMsg.BuildID, err)
		return
	}
}

// ProcessBuildCompletion processes a build completion message
func (api *StatusDashboardAPI) ProcessBuildCompletion(completionMsg message.BuildCompletionMessage) {
	ctx := context.Background()

	buildJSON, err := api.redisClient.Get(ctx, "build:"+completionMsg.BuildID).Result()
	if err != nil {
		log.Printf("Failed to get build %s: %v", completionMsg.BuildID, err)
		return
	}

	var build BuildStatus
	err = json.Unmarshal([]byte(buildJSON), &build)
	if err != nil {
		log.Printf("Failed to unmarshal build %s: %v", completionMsg.BuildID, err)
		return
	}

	build.Status = completionMsg.Status
	build.UpdatedAt = completionMsg.CompletedAt
	build.ArtifactURL = completionMsg.ArtifactURL

	buildJSONBytes, err := json.Marshal(build)
	if err != nil {
		log.Printf("Failed to marshal build %s: %v", completionMsg.BuildID, err)
		return
	}
	buildJSON = string(buildJSONBytes)

	err = api.redisClient.Set(ctx, "build:"+completionMsg.BuildID, buildJSON, 24*time.Hour).Err()
	if err != nil {
		log.Printf("Failed to save build %s: %v", completionMsg.BuildID, err)
		return
	}
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
		port = "8085"
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})
	defer redisClient.Close()

	kafkaConsumer, err := kafka.NewConsumer("kafka:29092", "status-dashboard-api")
	if err != nil {
		log.Fatalf("Failed to create Kafka consumer: %v", err)
	}
	defer kafkaConsumer.Close()

	err = kafkaConsumer.Subscribe([]string{"build-status", "build-logs", "build-completions"})
	if err != nil {
		log.Fatalf("Failed to subscribe to topics: %v", err)
	}

	api := NewStatusDashboardAPI(redisClient)

	go func() {
		kafkaConsumer.ConsumeMessages(func(key, value []byte) error {
			topic := string(key)

			switch topic {
			case "build-status":
				var statusMsg message.BuildStatusMessage
				if err := kafka.UnmarshalMessage(value, &statusMsg); err != nil {
					return err
				}
				api.ProcessBuildStatus(statusMsg)
			case "build-logs":
				var logMsg message.BuildLogMessage
				if err := kafka.UnmarshalMessage(value, &logMsg); err != nil {
					return err
				}
				api.ProcessBuildLog(logMsg)
			case "build-completions":
				var completionMsg message.BuildCompletionMessage
				if err := kafka.UnmarshalMessage(value, &completionMsg); err != nil {
					return err
				}
				api.ProcessBuildCompletion(completionMsg)
			}

			return nil
		})
	}()

	r := mux.NewRouter()

	// Add the CORS middleware right after it
	r.Use(corsMiddleware)

	r.HandleFunc("/api/builds", api.GetBuilds).Methods("GET")

	r.HandleFunc("/api/builds/{buildId}", api.GetBuild).Methods("GET")

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap handlers with CORS middleware
	handler := corsMiddleware(r)

	log.Printf("Status Dashboard API is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}
