package ace

import "time"

type BlockingMode string

const (
	BlockingPoll   BlockingMode = "polling"
	BlockingNotify BlockingMode = "notify"
)

type Config struct {
	Limits           Limits        `json:"limits"`
	Blocking         BlockingMode  `json:"blocking"`
	ScavengeInterval time.Duration `json:"scavenge_interval"`
}

func DefaultConfig() Config {
	return Config{
		Limits:           DefaultLimits(),
		Blocking:         BlockingNotify,
		ScavengeInterval: time.Hour,
	}
}
