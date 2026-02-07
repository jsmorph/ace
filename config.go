package ace

import "time"

// BlockingMode selects the blocking implementation for In and Rd.
type BlockingMode string

const (
	// BlockingPoll uses repeated queries with exponential backoff.
	BlockingPoll BlockingMode = "polling"
	// BlockingNotify uses Quamina to wake callers when a matching object arrives.
	BlockingNotify BlockingMode = "notify"
)

// Config controls Space behavior.
type Config struct {
	Limits                      Limits        `json:"limits"`
	Blocking                    BlockingMode  `json:"blocking"`
	ScavengeInterval            time.Duration `json:"scavenge_interval"`
	Deletes                     bool          `json:"deletes"`
	VisibilityTimeout           time.Duration `json:"visibility_timeout"`
	DBOperationTimeMonitorLimit time.Duration `json:"db_operation_time_monitor_limit"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Limits:                      DefaultLimits(),
		Blocking:                    BlockingNotify,
		ScavengeInterval:            time.Hour,
		VisibilityTimeout:           30 * time.Second,
		DBOperationTimeMonitorLimit: time.Second,
	}
}
