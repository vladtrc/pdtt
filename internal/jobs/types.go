package jobs

import "time"

const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"

	StageRenderingFrames = "rendering_frames"
	StageEncodingVideo   = "encoding_video"
	StageCompleted       = "completed"
	StageFailed          = "failed"
)

type Job struct {
	ID           string
	Scene        string
	Status       string
	Stage        string
	ErrorMessage string
	VideoPath    string
	VideoSize    int64
	CreatedAt    time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
	LeaseOwner   string
	LeaseExpires *time.Time
}
