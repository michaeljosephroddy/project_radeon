package feed

import (
	"os"
	"strings"
)

func isHomeFeedRankingEnabled() bool {
	value := strings.TrimSpace(os.Getenv("HOME_FEED_RANKING_ENABLED"))
	if value == "" {
		// Fall back to the older flag name so existing deployments do not lose ranking on upgrade.
		value = strings.TrimSpace(os.Getenv("FEED_FOR_YOU_ENABLED"))
	}
	return !isFeatureDisabled(value)
}

func isFeedReshareEnabled() bool {
	return !isFeatureDisabled(strings.TrimSpace(os.Getenv("FEED_RESHARES_ENABLED")))
}

func isFeatureDisabled(value string) bool {
	return strings.EqualFold(value, "false") || value == "0"
}
