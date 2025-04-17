package job

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/DeltaLaboratory/signing-server/pkg/models"
)

const (
	// CleanupDelay is the delay before the job is removed from the map and working directory is cleaned
	CleanupDelay = 5 * time.Minute
	// MaxFileSize is the maximum file size limit (1GB)
	MaxFileSize = 1024 * 1024 * 1024
)

var (
	jobMapLock = sync.RWMutex{}
	jobMap     = map[uuid.UUID]*models.Job{}
)

// CleanupJob removes the job from the map and its working directory after a delay
func CleanupJob(id uuid.UUID, workingDirectory string) {
	time.Sleep(CleanupDelay)
	log.Info().Str("job_id", id.String()).Msg("Cleaning up job")
	jobMapLock.Lock()
	delete(jobMap, id)
	jobMapLock.Unlock()
	if err := os.RemoveAll(workingDirectory); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Error().Err(err).Str("job_id", id.String()).Msg("Failed to cleanup working directory")
		}
	}
}

// ProcessSigningJob handles the actual signing process
func ProcessSigningJob(id uuid.UUID, cmd *exec.Cmd, jobWorkingDirectory string) {
	out, err := cmd.CombinedOutput()
	jobMapLock.Lock()
	defer jobMapLock.Unlock()
	if err != nil {
		jobMap[id].Processing = false
		jobMap[id].Success = false
		if out != nil {
			jobMap[id].Error = fmt.Sprintf("failed to sign file: %v: %s", err, out)
			log.Error().Err(err).Str("output", string(out)).Msg("Failed to sign file")
		} else {
			jobMap[id].Error = fmt.Sprintf("failed to sign file: %v", err)
			log.Error().Err(err).Msg("Failed to sign file")
		}
		log.Error().Str("job_id", id.String()).Msg("Job failed")
		_ = os.RemoveAll(jobWorkingDirectory)
		return
	}
	jobMap[id].Processing = false
	jobMap[id].Success = true
	log.Info().Str("job_id", id.String()).Str("output", string(out)).Msg("Job completed")
}

// CreateJob creates a new job and adds it to the job map
func CreateJob(id uuid.UUID, extension string) {
	jobMapLock.Lock()
	defer jobMapLock.Unlock()
	jobMap[id] = &models.Job{
		ID:         id,
		Processing: true,
		Success:    false,
		Extension:  extension,
	}
}

// GetJob retrieves a job from the job map
func GetJob(id uuid.UUID) (*models.Job, bool) {
	jobMapLock.RLock()
	defer jobMapLock.RUnlock()
	job, ok := jobMap[id]
	return job, ok
}
