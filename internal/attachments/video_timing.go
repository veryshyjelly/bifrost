package attachments

import (
	"math"
	"strconv"
	"strings"
)

// videoPacketDuration measures the presentation span, including the final frame.
// Both probe output and packet count are bounded independently of file metadata.
func videoPacketDuration(raw []byte, frameRate string) (float64, error) {
	start, end := math.Inf(1), math.Inf(-1)
	count := 0
	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		count++
		if count > 8192 {
			return 0, Unsupported()
		}
		pts, delay := "", ""
		for field := range strings.SplitSeq(line, "|") {
			key, value, _ := strings.Cut(field, "=")
			switch key {
			case "pts_time":
				pts = value
			case "duration_time":
				delay = value
			}
		}
		timestamp, err := strconv.ParseFloat(pts, 64)
		if err != nil || !finite(timestamp) {
			return 0, Unsupported()
		}
		duration := 0.0
		if delay != "" && delay != "N/A" {
			duration, err = strconv.ParseFloat(delay, 64)
			if err != nil || !finite(duration) || duration < 0 {
				return 0, Unsupported()
			}
		}
		if duration == 0 {
			fps := rate(frameRate)
			if !finite(fps) || fps <= 0 {
				return 0, Unsupported()
			}
			duration = 1 / fps
		}
		start = min(start, timestamp)
		end = max(end, timestamp+duration)
	}
	duration := end - start
	if count == 0 || !finite(duration) || duration <= 0 || duration > 60 {
		return 0, Unsupported()
	}
	return duration, nil
}
