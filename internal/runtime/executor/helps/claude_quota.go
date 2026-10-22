package helps

import (
	"encoding/json"
	"fmt"
)

// ClaudeUsageAvailable validates all reported usage windows before restoring quota routing.
func ClaudeUsageAvailable(body []byte) (bool, error) {
	var usage map[string]json.RawMessage
	if errJSON := json.Unmarshal(body, &usage); errJSON != nil {
		return false, fmt.Errorf("decode Claude usage: %w", errJSON)
	}
	if !claudeUsageWindowAvailable(usage["five_hour"]) || !claudeUsageWindowAvailable(usage["seven_day"]) {
		return false, nil
	}
	for _, name := range []string{"seven_day_oauth_apps", "seven_day_opus", "seven_day_sonnet", "seven_day_cowork", "seven_day_omelette", "seven_day_overage_included"} {
		window := usage[name]
		if len(window) != 0 && string(window) != "null" && !claudeUsageWindowAvailable(window) {
			return false, nil
		}
	}
	if raw := usage["cedar_ember"]; len(raw) != 0 && string(raw) != "null" {
		var grants struct {
			AtLimit   *bool    `json:"at_limit"`
			Exhausted []string `json:"exhausted"`
		}
		if json.Unmarshal(raw, &grants) != nil || grants.AtLimit == nil || *grants.AtLimit || len(grants.Exhausted) > 0 {
			return false, nil
		}
	}
	if raw := usage["limits"]; len(raw) != 0 {
		var limits []struct {
			Percent *float64 `json:"percent"`
		}
		if json.Unmarshal(raw, &limits) != nil {
			return false, nil
		}
		for _, limit := range limits {
			if limit.Percent == nil || *limit.Percent < 0 || *limit.Percent >= 100 {
				return false, nil
			}
		}
	}
	return true, nil
}

func claudeUsageWindowAvailable(raw json.RawMessage) bool {
	var window struct {
		Utilization  *float64 `json:"utilization"`
		LockedReason *string  `json:"locked_reason"`
	}
	return json.Unmarshal(raw, &window) == nil && window.Utilization != nil &&
		*window.Utilization >= 0 && *window.Utilization < 100 &&
		(window.LockedReason == nil || *window.LockedReason == "")
}
