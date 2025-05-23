// shared/model/build.go
package model

import (
	"time"
)

type BuildStatus struct {
	ID            string     `json:"id"`
	RepositoryURL string     `json:"repository_url"`
	Branch        string     `json:"branch,omitempty"`
	CommitHash    string     `json:"commit_hash,omitempty"`
	UserID        string     `json:"user_id"`
	Status        string     `json:"status"` // queued, in-progress, completed, failed
	Message       string     `json:"message,omitempty"`
	ArtifactURL   string     `json:"artifact_url,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Duration      int64      `json:"duration,omitempty"` // in milliseconds
}
