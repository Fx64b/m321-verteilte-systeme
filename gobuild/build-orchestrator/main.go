package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"gobuild/shared/kafka"
	"gobuild/shared/message"
)

// BuildJob represents a build job
type BuildJob struct {
	ID            string    `json:"id"`
	RepositoryURL string    `json:"repository_url"`
	Branch        string    `json:"branch"`
	CommitHash    string    `json:"commit_hash"`
	UserID        string    `json:"user_id"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// BuildOrchestrator manages build jobs
type BuildOrchestrator struct {
	jobs          map[string]*BuildJob
	mutex         sync.RWMutex
	kafkaProducer *kafka.Producer
	redisClient   *redis.Client
}

// NewBuildOrchestrator creates a new BuildOrchestrator
func NewBuildOrchestrator(kafkaProducer *kafka.Producer, redisClient *redis.Client) *BuildOrchestrator {
	return &BuildOrchestrator{
		jobs:          make(map[string]*BuildJob),
		kafkaProducer: kafkaProducer,
		redisClient:   redisClient,
	}
}

// ProcessBuildRequest processes a build request
func (bo *BuildOrchestrator) ProcessBuildRequest(buildReq message.BuildRequestMessage) error {
	// Create a new build job
	job := &BuildJob{
		ID:            buildReq.ID,
		RepositoryURL: buildReq.RepositoryURL,
		Branch:        buildReq.Branch,
		CommitHash:    buildReq.CommitHash,
		UserID:        buildReq.UserID,
		Status:        "queued",
		CreatedAt:     buildReq.CreatedAt,
		UpdatedAt:     time.Now(),
	}

	// Store the job in memory
	bo.mutex.Lock()
	bo.jobs[job.ID] = job
	bo.mutex.Unlock()

	// Store the job in Redis for persistence
	jobJSON, err := json.Marshal(job)
	if err != nil {
		return err
	}
	ctx := context.Background()
	err = bo.redisClient.Set(ctx, fmt.Sprintf("build:%s", job.ID), jobJSON, 24*time.Hour).Err()
	if err != nil {
		return err
	}

	// Send a status update
	statusMsg := message.BuildStatusMessage{
		BuildID:   job.ID,
		Status:    job.Status,
		Message:   "Build queued",
		UpdatedAt: job.UpdatedAt,
	}
	err = bo.kafkaProducer.SendMessage("build-status", job.ID, statusMsg)
	if err != nil {
		return err
	}

	// Forward the job to the builder
	return bo.kafkaProducer.SendMessage("build-requests", job.ID, buildReq)
}

// UpdateBuildStatus updates the status of a build job
func (bo *BuildOrchestrator) UpdateBuildStatus(buildID, status, msg string) error {
	bo.mutex.Lock()
	defer bo.mutex.Unlock()

	job, exists := bo.jobs[buildID]
	if !exists {
		return fmt.Errorf("build job not found: %s", buildID)
	}

	job.Status = status
	job.UpdatedAt = time.Now()

	// Store the updated job in Redis
	jobJSON, err := json.Marshal(job)
	if err != nil {
		return err
	}
	ctx := context.Background()
	err = bo.redisClient.Set(ctx, fmt.Sprintf("build:%s", job.ID), jobJSON, 24*time.Hour).Err()
	if err != nil {
		return err
	}

	// Send a status update
	statusMsg := message.BuildStatusMessage{
		BuildID:   job.ID,
		Status:    job.Status,
		Message:   msg,
		UpdatedAt: job.UpdatedAt,
	}
	return bo.kafkaProducer.SendMessage("build-status", job.ID, statusMsg)
}

// GetBuildJob retrieves a build job by ID
func (bo *BuildOrchestrator) GetBuildJob(buildID string) (*BuildJob, error) {
	bo.mutex.RLock()
	job, exists := bo.jobs[buildID]
	bo.mutex.RUnlock()

	if exists {
		return job, nil
	}

	// Try to get from Redis
	ctx := context.Background()
	jobJSON, err := bo.redisClient.Get(ctx, fmt.Sprintf("build:%s", buildID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("build job not found: %s", buildID)
		}
		return nil, err
	}

	// Use a different variable name to avoid redeclaration
	var retrievedJob BuildJob
	err = json.Unmarshal([]byte(jobJSON), &retrievedJob)
	if err != nil {
		return nil, err
	}

	// Cache the job in memory
	bo.mutex.Lock()
	bo.jobs[retrievedJob.ID] = &retrievedJob
	bo.mutex.Unlock()

	return &retrievedJob, nil
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	kafkaProducer, err := kafka.NewProducer("kafka:29092")
	if err != nil {
		log.Fatalf("Failed to create Kafka producer: %v", err)
	}
	defer kafkaProducer.Close()

	kafkaConsumer, err := kafka.NewConsumer("kafka:29092", "build-orchestrator")
	if err != nil {
		log.Fatalf("Failed to create Kafka consumer: %v", err)
	}
	defer kafkaConsumer.Close()

	// Subscribe to build request topic
	err = kafkaConsumer.Subscribe([]string{"build-requests"})
	if err != nil {
		log.Fatalf("Failed to subscribe to topics: %v", err)
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})
	defer redisClient.Close()

	orchestrator := NewBuildOrchestrator(kafkaProducer, redisClient)

	// Start consuming build requests
	go func() {
		kafkaConsumer.ConsumeMessages(func(key, value []byte) error {
			var buildReq message.BuildRequestMessage
			if err := kafka.UnmarshalMessage(value, &buildReq); err != nil {
				return err
			}

			log.Printf("Received build request: %s", buildReq.ID)
			return orchestrator.ProcessBuildRequest(buildReq)
		})
	}()

	r := mux.NewRouter()

	r.HandleFunc("/api/builds/{buildId}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		buildID := vars["buildId"]

		job, err := orchestrator.GetBuildJob(buildID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)
	}).Methods("GET")

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("Build Orchestrator Service is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
