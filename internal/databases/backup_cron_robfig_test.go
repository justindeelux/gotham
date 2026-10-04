package databases

import (
	"errors"
	"testing"
	"time"
)

// TestNextCronTimeRobfigRegression pins the schedule results across the
// hand-written parser to robfig/cron migration (JUS-23). Expected times were
// produced by the frozen pre-migration implementation and verified by a
// 2080-case differential test with zero differences; they now guard the
// robfig-backed implementation, especially the day-of-week 7 spelling, the
// day-of-month/day-of-week OR rule, leap days, and the DST wall-clock
// policy (repeated hour fires once, gap slots fire at the transition).
func TestNextCronTimeRobfigRegression(t *testing.T) {
	utc := time.UTC
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	tests := []struct {
		name       string
		expression string
		after      time.Time
		loc        *time.Location
		want       time.Time
	}{
		{"daily", "0 2 * * *", time.Date(2026, 9, 27, 1, 59, 0, 0, utc), utc,
			time.Date(2026, 9, 27, 2, 0, 0, 0, utc)},
		{"step hour", "0 */6 * * *", time.Date(2026, 9, 27, 7, 30, 0, 0, utc), utc,
			time.Date(2026, 9, 27, 12, 0, 0, 0, utc)},
		{"dow 7 is sunday", "0 3 * * 7", time.Date(2026, 9, 27, 4, 0, 0, 0, utc), utc,
			time.Date(2026, 10, 4, 3, 0, 0, 0, utc)},
		{"dow range into 7", "0 9 * * 5-7", time.Date(2026, 9, 27, 10, 0, 0, 0, utc), utc,
			time.Date(2026, 10, 2, 9, 0, 0, 0, utc)},
		{"dom or dow", "30 4 1 * 1", time.Date(2026, 9, 27, 0, 0, 0, 0, utc), utc,
			time.Date(2026, 9, 28, 4, 30, 0, 0, utc)},
		{"leap day", "0 0 29 2 *", time.Date(2023, 1, 1, 0, 0, 0, 0, utc), utc,
			time.Date(2024, 2, 29, 0, 0, 0, 0, utc)},
		{"value step", "5/2 * * * *", time.Date(2026, 9, 27, 12, 4, 0, 0, utc), utc,
			time.Date(2026, 9, 27, 12, 5, 0, 0, utc)},
		{"dow stride misses 7", "0 0 * * 6/2", time.Date(2026, 9, 27, 10, 0, 0, 0, utc), utc,
			time.Date(2026, 10, 3, 0, 0, 0, 0, utc)},
		{"dow stride lands on 7", "0 0 * * 5/2", time.Date(2026, 9, 27, 10, 0, 0, 0, utc), utc,
			time.Date(2026, 10, 2, 0, 0, 0, 0, utc)},
		{"dow range stride into 7", "0 0 * * 1-7/2", time.Date(2026, 9, 27, 10, 0, 0, 0, utc), utc,
			time.Date(2026, 9, 28, 0, 0, 0, 0, utc)},
		{"seconds truncate", "* * * * *", time.Date(2026, 9, 27, 12, 21, 33, 0, utc), utc,
			time.Date(2026, 9, 27, 12, 22, 0, 0, utc)},
		{"month rollover", "0 0 1 * *", time.Date(2026, 1, 31, 23, 45, 0, 0, utc), utc,
			time.Date(2026, 2, 1, 0, 0, 0, 0, utc)},
		{"yearly", "59 23 31 12 *", time.Date(2026, 6, 1, 0, 0, 0, 0, utc), utc,
			time.Date(2026, 12, 31, 23, 59, 0, 0, utc)},
		{"fall-back first occurrence", "30 1 * * *", time.Date(2025, 11, 1, 12, 0, 0, 0, ny), ny,
			time.Date(2025, 11, 2, 1, 30, 0, 0, ny)},
		{"gap fires at transition", "30 2 * * *", time.Date(2025, 3, 8, 12, 0, 0, 0, ny), ny,
			time.Date(2025, 3, 9, 3, 0, 0, 0, ny)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := nextCronTime(test.expression, test.after, test.loc)
			if err != nil {
				t.Fatalf("nextCronTime(%q): %v", test.expression, err)
			}
			if !got.Equal(test.want) {
				t.Errorf("next = %v, want %v", got, test.want)
			}
		})
	}

	if _, err := nextCronTime("0 0 31 4 *", time.Date(2026, 1, 1, 0, 0, 0, 0, utc), utc); err == nil {
		t.Error("April 31st must be rejected instead of looping forever")
	}
}

// TestParseCronKeepsNumericGrammar pins the deliberately numeric-only
// grammar: robfig would accept month/weekday names, descriptors and "?",
// which stay rejected with ErrValidation so the API keeps answering 400
// with the historical messages.
func TestParseCronKeepsNumericGrammar(t *testing.T) {
	valid := []string{
		"0 2 * * *",
		"0 3 * * 7",
		"0 9 * * 5-7",
		"0 0 * * 1-7/2",
		"5/2 * * * *",
		"*/15 0-6 1,15 */3 1-5",
	}
	for _, expression := range valid {
		if _, err := parseCron(expression); err != nil {
			t.Errorf("parseCron(%q) = %v, want success", expression, err)
		}
	}
	invalid := []string{
		"0 2 * JAN *",
		"0 2 * * MON",
		"0 0 1 JAN MON",
		"@daily",
		"@hourly",
		"0 2 * * ?",
		"0 2 ? * *",
		"0 0 12 * * *",
		"0 0 * * * *",
		"TZ=America/New_York 0 2 * * *",
		"0 0 * * sun",
	}
	for _, expression := range invalid {
		if _, err := parseCron(expression); err == nil {
			t.Errorf("parseCron(%q) = nil, want an error", expression)
		} else if !errors.Is(err, ErrValidation) {
			t.Errorf("parseCron(%q) err = %v, want ErrValidation", expression, err)
		}
	}
}

// TestNormalizeDowItem pins the day-of-week 7 rewrite feeding robfig's 0-6
// range: every output must expand to the same value set the old parser
// canonicalised (7 is Sunday).
func TestNormalizeDowItem(t *testing.T) {
	cases := map[string]string{
		"*":     "*",
		"*/2":   "*/2",
		"7":     "0",
		"5":     "5",
		"5-7":   "5-6,0",
		"7-7":   "0",
		"1-7/2": "1-6/2,0",
		"2-7/2": "2-6/2",
		"5/2":   "5-6/2,0",
		"4/2":   "4-6/2",
		"6/2":   "6-6/2",
		"7/2":   "0",
		"0-6":   "0-6",
	}
	for input, want := range cases {
		got, _, err := checkCronField(input, 0, 7, true)
		if err != nil {
			t.Fatalf("checkCronField(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("checkCronField(%q) = %q, want %q", input, got, want)
		}
	}
}
