package jobs

import "time"

const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"

	StageQueued          = "queued"
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
	QueuePos     int
	ErrorMessage string
	VideoPath    string
	VideoSize    int64
	CreatedAt    time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
	LeaseOwner   string
	LeaseExpires *time.Time
}

type StatusView struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	Stage       string     `json:"stage"`
	QueuePos    int        `json:"queue_position"`
	Error       string     `json:"error,omitempty"`
	VideoURL    string     `json:"video_url,omitempty"`
	VideoSize   int64      `json:"video_size,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	ElapsedSec  float64    `json:"elapsed_sec"`
}
