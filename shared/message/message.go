package message

import (
	"time"
)

type BuildRequestMessage struct {
	ID            string    `json:"id"`
	RepositoryURL string    `json:"repository_url"`
	Branch        string    `json:"branch"`
	CommitHash    string    `json:"commit_hash"`
	UserID        string    `json:"user_id"`
	CreatedAt     time.Time `json:"created_at"`
}

type BuildStatusMessage struct {
	BuildID   string    `json:"build_id"`
	Status    string    `json:"status"` // queued, in-progress, completed, failed
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BuildLogMessage struct {
	BuildID   string    `json:"build_id"`
	LogEntry  string    `json:"log_entry"`
	Timestamp time.Time `json:"timestamp"`
}

type BuildCompletionMessage struct {
	BuildID     string    `json:"build_id"`
	Status      string    `json:"status"` // success or failure
	ArtifactURL string    `json:"artifact_url,omitempty"`
	Duration    int64     `json:"duration"` // in milliseconds
	CompletedAt time.Time `json:"completed_at"`
}
