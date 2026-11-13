package attachments

import (
	"math"
	"strings"
	"testing"
)

func TestVideoPacketDuration(t *testing.T) {
	for _, tc := range []struct {
		name, packets, fps string
		want               float64
	}{
		{"variable timing", "pts_time=2|duration_time=0.05\npts_time=2.05|duration_time=0.15\n", "30/1", .2},
		{"presentation order", "pts_time=2.05|duration_time=0.15\npts_time=2|duration_time=0.05\n", "30/1", .2},
		{"missing last duration", "pts_time=0|duration_time=0.04\npts_time=0.04|duration_time=N/A\n", "25/1", .08},
		{"zero duration", "duration_time=0|pts_time=-1\n", "25/1", .04},
		{"single frame", "pts_time=0|duration_time=0.1\n", "0/0", .1},
		{"empty", "", "30/1", 0},
		{"missing timestamp", "pts_time=N/A|duration_time=.1", "30/1", 0},
		{"nonfinite timestamp", "pts_time=NaN|duration_time=.1", "30/1", 0},
		{"nonfinite duration", "pts_time=0|duration_time=Inf", "30/1", 0},
		{"negative duration", "pts_time=0|duration_time=-1", "30/1", 0},
		{"missing timing", "pts_time=0|duration_time=N/A", "0/0", 0},
		{"invalid duration", "pts_time=0|duration_time=broken", "30/1", 0},
		{"too long", "pts_time=0|duration_time=.1\npts_time=60|duration_time=.1", "30/1", 0},
		{"too many packets", strings.Repeat("pts_time=0|duration_time=.1\n", 8193), "30/1", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := videoPacketDuration([]byte(tc.packets), tc.fps)
			if tc.want == 0 {
				if err == nil {
					t.Fatal("invalid timing accepted", got)
				}
				return
			}
			if err != nil || math.Abs(got-tc.want) > .000001 {
				t.Fatal(got, err)
			}
		})
	}
}
