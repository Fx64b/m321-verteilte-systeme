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
	log.Printf("🔄 Processing build request: %s for repo: %s", buildReq.ID, buildReq.RepositoryURL)

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
		log.Printf("❌ Failed to marshal job: %v", err)
		return err
	}
	ctx := context.Background()
	err = bo.redisClient.Set(ctx, fmt.Sprintf("build:%s", job.ID), jobJSON, 24*time.Hour).Err()
	if err != nil {
		log.Printf("❌ Failed to store job in Redis: %v", err)
		return err
	}

	log.Printf("✅ Stored job %s in Redis", job.ID)

	// Also store initial build info for status-dashboard-api
	buildStatus := map[string]interface{}{
		"id":             job.ID,
		"repository_url": job.RepositoryURL,
		"branch":         job.Branch,
		"commit_hash":    job.CommitHash,
		"status":         job.Status,
		"message":        "Build queued",
		"created_at":     job.CreatedAt,
		"updated_at":     job.UpdatedAt,
	}

	buildStatusJSON, err := json.Marshal(buildStatus)
	if err != nil {
		log.Printf("❌ Failed to marshal build status: %v", err)
		return err
	}

	// Store in the same format that status-dashboard-api expects
	err = bo.redisClient.Set(ctx, fmt.Sprintf("build:status:%s", job.ID), buildStatusJSON, 24*time.Hour).Err()
	if err != nil {
		log.Printf("❌ Failed to store build status: %v", err)
	}

	// Send a status update
	statusMsg := message.BuildStatusMessage{
		BuildID:   job.ID,
		Status:    job.Status,
		Message:   "Build queued for processing",
		UpdatedAt: job.UpdatedAt,
	}
	err = bo.kafkaProducer.SendMessage("build-status", job.ID, statusMsg)
	if err != nil {
		log.Printf("❌ Failed to send status update: %v", err)
		return err
	}

	log.Printf("✅ Sent status update for job %s", job.ID)

	log.Printf("📤 Sending build job %s to build-jobs topic", job.ID)
	// Forward the job to the builder - send to build-jobs topic
	err = bo.kafkaProducer.SendMessage("build-jobs", job.ID, buildReq)
	if err != nil {
		log.Printf("❌ Failed to send to build-jobs topic: %v", err)
		return err
	}

	log.Printf("✅ Successfully sent build job %s to build-jobs topic", job.ID)
	return nil
}

// ProcessBuildStatus processes status updates from builders
func (bo *BuildOrchestrator) ProcessBuildStatus(statusMsg message.BuildStatusMessage) error {
	log.Printf("📊 Processing build status update: %s - %s", statusMsg.BuildID, statusMsg.Status)

	bo.mutex.Lock()
	defer bo.mutex.Unlock()

	job, exists := bo.jobs[statusMsg.BuildID]
	if !exists {
		// Try to load from Redis
		ctx := context.Background()
		jobJSON, err := bo.redisClient.Get(ctx, fmt.Sprintf("build:%s", statusMsg.BuildID)).Result()
		if err != nil {
			log.Printf("⚠️ Build job not found: %s", statusMsg.BuildID)
			return nil // Don't return error, just log it
		}

		var loadedJob BuildJob
		err = json.Unmarshal([]byte(jobJSON), &loadedJob)
		if err != nil {
			return err
		}
		bo.jobs[statusMsg.BuildID] = &loadedJob
		job = &loadedJob
	}

	job.Status = statusMsg.Status
	job.UpdatedAt = statusMsg.UpdatedAt

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

	// Forward the status update to other services
	return bo.kafkaProducer.SendMessage("build-status", statusMsg.BuildID, statusMsg)
}

// ProcessBuildCompletion processes build completion messages
func (bo *BuildOrchestrator) ProcessBuildCompletion(completionMsg message.BuildCompletionMessage) error {
	log.Printf("🏁 Processing build completion: %s - %s", completionMsg.BuildID, completionMsg.Status)

	bo.mutex.Lock()
	defer bo.mutex.Unlock()

	job, exists := bo.jobs[completionMsg.BuildID]
	if !exists {
		// Try to load from Redis
		ctx := context.Background()
		jobJSON, err := bo.redisClient.Get(ctx, fmt.Sprintf("build:%s", completionMsg.BuildID)).Result()
		if err != nil {
			log.Printf("⚠️ Build job not found: %s", completionMsg.BuildID)
			return nil
		}

		var loadedJob BuildJob
		err = json.Unmarshal([]byte(jobJSON), &loadedJob)
		if err != nil {
			return err
		}
		bo.jobs[completionMsg.BuildID] = &loadedJob
		job = &loadedJob
	}

	job.Status = completionMsg.Status
	job.UpdatedAt = completionMsg.CompletedAt

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

	// Forward the completion message to other services
	return bo.kafkaProducer.SendMessage("build-completions", completionMsg.BuildID, completionMsg)
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
		port = "8082"
	}

	log.Println("🚀 Starting Build Orchestrator...")

	kafkaProducer, err := kafka.NewProducer("kafka:29092")
	if err != nil {
		log.Fatalf("❌ Failed to create Kafka producer: %v", err)
	}
	defer kafkaProducer.Close()
	log.Println("✅ Kafka producer created")

	redisClient := redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})
	defer redisClient.Close()
	log.Println("✅ Redis client created")

	// Test Redis connection
	ctx := context.Background()
	_, err = redisClient.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}
	log.Println("✅ Redis connection verified")

	orchestrator := NewBuildOrchestrator(kafkaProducer, redisClient)

	// Simple single consumer approach - let's go back to this to debug
	kafkaConsumer, err := kafka.NewConsumer("kafka:29092", "build-orchestrator")
	if err != nil {
		log.Fatalf("❌ Failed to create Kafka consumer: %v", err)
	}
	defer kafkaConsumer.Close()
	log.Println("✅ Kafka consumer created")

	// Subscribe to all relevant topics
	err = kafkaConsumer.Subscribe([]string{"build-requests"})
	if err != nil {
		log.Fatalf("❌ Failed to subscribe to topics: %v", err)
	}
	log.Println("✅ Subscribed to build-requests topic")

	// Start consuming messages from Kafka
	go func() {
		log.Println("🎧 Starting to consume messages from Kafka...")
		kafkaConsumer.ConsumeMessages(func(key, value []byte) error {
			log.Printf("📨 Received message with key: %s", string(key))
			log.Printf("📨 Message content: %s", string(value))

			// Try to unmarshal as build request
			var buildReq message.BuildRequestMessage
			if err := kafka.UnmarshalMessage(value, &buildReq); err != nil {
				log.Printf("❌ Failed to unmarshal as BuildRequestMessage: %v", err)
				return err
			}

			if buildReq.ID != "" {
				log.Printf("📬 Received build request: %s", buildReq.ID)
				return orchestrator.ProcessBuildRequest(buildReq)
			}

			log.Printf("⚠️ Received message but couldn't identify type")
			return nil
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

	log.Printf("🌐 Build Orchestrator Service is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
