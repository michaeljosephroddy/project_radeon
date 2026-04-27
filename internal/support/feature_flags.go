package support

import (
	"os"
	"strconv"
	"strings"
	"time"
)

func isSupportRoutingEnabled() bool {
	return !isFeatureDisabled(strings.TrimSpace(os.Getenv("SUPPORT_ROUTING_ENABLED")))
}

func SupportRoutingSweepInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("SUPPORT_ROUTING_SWEEP_INTERVAL_SECONDS"))
	if value == "" {
		return 30 * time.Second
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func isFeatureDisabled(value string) bool {
	return strings.EqualFold(value, "false") || value == "0"
}
