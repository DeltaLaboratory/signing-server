package models

import (
	"github.com/google/uuid"
)

// Job represents a signing job
type Job struct {
	ID         uuid.UUID `json:"id"`
	Processing bool      `json:"processing"`
	Success    bool      `json:"success"`
	Error      string    `json:"error"`
	Extension  string    `json:"extension"`
}

// CreateJobResponse represents the response for job creation
type CreateJobResponse struct {
	ID uuid.UUID `json:"id"`
}