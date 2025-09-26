package utils

import (
	"fmt"
	"time"
)

// FormatDuration formats a duration into a human-readable string with hours, minutes, and seconds
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	
	var parts []string
	
	if hours > 0 {
		if hours == 1 {
			parts = append(parts, "1h")
		} else {
			parts = append(parts, fmt.Sprintf("%dh", hours))
		}
	}
	
	if minutes > 0 {
		if minutes == 1 {
			parts = append(parts, "1m")
		} else {
			parts = append(parts, fmt.Sprintf("%dm", minutes))
		}
	}
	
	if seconds > 0 || len(parts) == 0 {
		if seconds == 1 {
			parts = append(parts, "1s")
		} else {
			parts = append(parts, fmt.Sprintf("%ds", seconds))
		}
	}
	
	// Join parts with space
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += " "
		}
		result += part
	}
	
	return result
}

// FormatDurationVerbose formats a duration into a more verbose human-readable string
func FormatDurationVerbose(d time.Duration) string {
	if d == 0 {
		return "0 seconds"
	}
	
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	
	var parts []string
	
	if hours > 0 {
		if hours == 1 {
			parts = append(parts, "1 hour")
		} else {
			parts = append(parts, fmt.Sprintf("%d hours", hours))
		}
	}
	
	if minutes > 0 {
		if minutes == 1 {
			parts = append(parts, "1 minute")
		} else {
			parts = append(parts, fmt.Sprintf("%d minutes", minutes))
		}
	}
	
	if seconds > 0 || len(parts) == 0 {
		if seconds == 1 {
			parts = append(parts, "1 second")
		} else {
			parts = append(parts, fmt.Sprintf("%d seconds", seconds))
		}
	}
	
	// Join parts with commas and "and"
	if len(parts) == 1 {
		return parts[0]
	}
	
	if len(parts) == 2 {
		return parts[0] + " and " + parts[1]
	}
	
	result := ""
	for i, part := range parts {
		if i == len(parts)-1 {
			result += "and " + part
		} else if i > 0 {
			result += ", " + part
		} else {
			result += part
		}
	}
	
	return result
}

// FormatDurationCompact formats a duration into a compact format (e.g., "1h 30m", "45m 12s")
func FormatDurationCompact(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	
	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
	
	if minutes > 0 {
		if seconds > 0 {
			return fmt.Sprintf("%dm %ds", minutes, seconds)
		}
		return fmt.Sprintf("%dm", minutes)
	}
	
	return fmt.Sprintf("%ds", seconds)
}

// GetUptimeString returns a formatted uptime string
func GetUptimeString(startTime time.Time) string {
	uptime := time.Since(startTime)
	return FormatDuration(uptime)
}

// GetUptimeStringVerbose returns a verbose formatted uptime string
func GetUptimeStringVerbose(startTime time.Time) string {
	uptime := time.Since(startTime)
	return FormatDurationVerbose(uptime)
}

// GetUptimeStringCompact returns a compact formatted uptime string
func GetUptimeStringCompact(startTime time.Time) string {
	uptime := time.Since(startTime)
	return FormatDurationCompact(uptime)
}