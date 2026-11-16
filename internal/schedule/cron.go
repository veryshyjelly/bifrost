// Package schedule preserves Bifrost's numeric five-field cron contract.
// It evaluates due minutes only; durable scheduling, deduplication and dispatch
// are responsibilities of the metadata coordinator and its store.
package schedule

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
)

// Cron retains the configured expression and timezone alongside its parsed form.
// An off schedule has no parsed matcher and never matches a minute.
type Cron struct {
	expression, zone string
	schedule         cron.Schedule
}

var cronField = regexp.MustCompile(`^(?:\*|[0-9]+(?:-[0-9]+)?)(?:/[0-9]+)?$`)
var ErrInvalid = errors.New("invalid numeric five-field cron expression or timezone")

// New accepts numeric five-field expressions or "off". Empty settings default to
// hourly execution in Asia/Kolkata. Enabled schedules reject invalid timezones
// and Local so host configuration cannot change their meaning.
func New(expression, zone string) (*Cron, error) {
	if expression == "" {
		expression = "0 * * * *"
	}
	if zone == "" {
		zone = "Asia/Kolkata"
	}
	c := &Cron{expression: expression, zone: zone}
	if expression == "off" {
		return c, nil
	}
	if zone == "Local" {
		return nil, ErrInvalid
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return nil, ErrInvalid
	}
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return nil, ErrInvalid
	}
	mins, maxs := []int{0, 0, 1, 1, 0}, []int{59, 23, 31, 12, 7}
	normalized := make([]string, 5)
	for i, part := range parts {
		if part == "*" {
			normalized[i] = "*"
			continue
		}
		values := map[int]bool{}
		for _, item := range strings.Split(part, ",") {
			if !cronField.MatchString(item) {
				return nil, ErrInvalid
			}
			pieces := strings.Split(item, "/")
			step := 1
			if len(pieces) == 2 {
				n, err := strconv.Atoi(pieces[1])
				if err != nil {
					// The grammar already guarantees decimal digits. Any step
					// exceeding int also exceeds every field's range; like the
					// baseline matcher, retain only that range's first value.
					n = maxs[i] + 1
				}
				if n < 1 {
					return nil, ErrInvalid
				}
				step = n
			}
			low, high := mins[i], maxs[i]
			if pieces[0] != "*" {
				ends := strings.Split(pieces[0], "-")
				var err error
				low, err = strconv.Atoi(ends[0])
				if err != nil {
					return nil, ErrInvalid
				}
				if len(ends) == 2 {
					high, err = strconv.Atoi(ends[1])
					if err != nil {
						return nil, ErrInvalid
					}
				} else if len(pieces) == 1 {
					high = low
				}
			}
			if low < mins[i] || high > maxs[i] || low > high {
				return nil, ErrInvalid
			}
			for n := low; n <= high; {
				value := n
				if i == 4 && n == 7 {
					value = 0
				}
				values[value] = true
				if step > high-n {
					break
				}
				n += step
			}
		}
		sorted := make([]int, 0, len(values))
		for n := range values {
			sorted = append(sorted, n)
		}
		sort.Ints(sorted)
		numbers := make([]string, 0, len(sorted))
		for _, n := range sorted {
			numbers = append(numbers, strconv.Itoa(n))
		}
		normalized[i] = strings.Join(numbers, ",")
	}
	parsed, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse("CRON_TZ=" + zone + " " + strings.Join(normalized, " "))
	if err != nil {
		return nil, ErrInvalid
	}
	c.schedule = parsed
	return c, nil
}

// Matches tests the minute containing now, ignoring seconds and nanoseconds.
// It is safe on nil and disabled schedules.
func (c *Cron) Matches(now time.Time) bool {
	minute := now.Truncate(time.Minute)
	return c != nil && c.schedule != nil && c.schedule.Next(minute.Add(-time.Nanosecond)).Equal(minute)
}
func (c *Cron) Expression() string { return c.expression }
func (c *Cron) Timezone() string   { return c.zone }
