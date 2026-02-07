package ace

import "time"

type BlockingMode string

const (
	BlockingPoll   BlockingMode = "polling"
	BlockingNotify BlockingMode = "notify"
)

type Config struct {
	Limits                      Limits        `json:"limits"`
	Blocking                    BlockingMode  `json:"blocking"`
	ScavengeInterval            time.Duration `json:"scavenge_interval"`
	Deletes                     bool          `json:"deletes"`
	VisibilityTimeout           time.Duration `json:"visibility_timeout"`
	DBOperationTimeMonitorLimit time.Duration `json:"db_operation_time_monitor_limit"`
}

func DefaultConfig() Config {
	return Config{
		Limits:                      DefaultLimits(),
		Blocking:                    BlockingNotify,
		ScavengeInterval:            time.Hour,
		VisibilityTimeout:           30 * time.Second,
		DBOperationTimeMonitorLimit: time.Second,
	}
}
