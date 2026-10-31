package cronexpr

import (
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	valid := []string{
		"0 2 * * *",
		"*/6 * * * *",
		"0 3 * * 0",
		"0 3 * * 7",
		"5,20,35 0-6 * * 1-5",
		"15/10 * * * *",
		"0 0 1 1 *",
	}
	for _, expression := range valid {
		if err := Validate(expression); err != nil {
			t.Errorf("Validate(%q) = %v, want success", expression, err)
		}
	}

	invalid := []string{
		"",
		"* * *",
		"61 * * * *",
		"* 24 * * *",
		"0 0 32 * *",
		"0 0 * 13 *",
		"0 0 * * 8",
		"0 2 * * */0",
		"0 2 * * -1",
		"a b c d e",
	}
	for _, expression := range invalid {
		if err := Validate(expression); err == nil {
			t.Errorf("Validate(%q) = nil, want an error", expression)
		}
	}
}

func TestNext(t *testing.T) {
	utc := time.UTC
	tests := []struct {
		name       string
		expression string
		after      time.Time
		want       time.Time
	}{
		{
			name:       "daily later the same day",
			expression: "0 2 * * *",
			after:      time.Date(2026, 9, 27, 1, 59, 0, 0, utc),
			want:       time.Date(2026, 9, 27, 2, 0, 0, 0, utc),
		},
		{
			name:       "daily rolls to tomorrow",
			expression: "0 2 * * *",
			after:      time.Date(2026, 9, 27, 3, 0, 0, 0, utc),
			want:       time.Date(2026, 9, 28, 2, 0, 0, 0, utc),
		},
		{
			name:       "every six hours",
			expression: "0 */6 * * *",
			after:      time.Date(2026, 9, 27, 7, 30, 0, 0, utc),
			want:       time.Date(2026, 9, 27, 12, 0, 0, 0, utc),
		},
		{
			name:       "sunday at three",
			expression: "0 3 * * 0",
			after:      time.Date(2026, 9, 27, 4, 0, 0, 0, utc),
			want:       time.Date(2026, 10, 4, 3, 0, 0, 0, utc),
		},
		{
			name:       "weekday range",
			expression: "0 9 * * 1-5",
			after:      time.Date(2026, 9, 27, 10, 0, 0, 0, utc),
			want:       time.Date(2026, 9, 28, 9, 0, 0, 0, utc),
		},
		{
			name:       "list of minutes",
			expression: "5,20,35 * * * *",
			after:      time.Date(2026, 9, 27, 12, 21, 0, 0, utc),
			want:       time.Date(2026, 9, 27, 12, 35, 0, 0, utc),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Next(test.expression, test.after, utc)
			if err != nil {
				t.Fatalf("Next(%q): %v", test.expression, err)
			}
			if !got.Equal(test.want) {
				t.Errorf("next = %v, want %v", got, test.want)
			}
			if !got.After(test.after) {
				t.Errorf("next = %v must be strictly after %v", got, test.after)
			}
		})
	}
}

func TestNextRejectsImpossibleExpressions(t *testing.T) {
	if _, err := Next("0 0 30 2 *", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC); err == nil {
		t.Error("30 February must be rejected instead of looping forever")
	}
	if _, err := Next("bogus", time.Now(), time.UTC); err == nil {
		t.Error("expected a parse error")
	}
}

func TestNextRunNeverReusesAServedSlot(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	fallSlot, err := Next("30 1 * * *", time.Date(2025, 11, 1, 12, 0, 0, 0, loc), loc)
	if err != nil {
		t.Fatalf("fall Next: %v", err)
	}
	nextSlot, err := NextRun("30 1 * * *", fallSlot, fallSlot.Add(10*time.Minute), loc)
	if err != nil {
		t.Fatalf("fall NextRun: %v", err)
	}
	if want := time.Date(2025, 11, 3, 1, 30, 0, 0, loc); !nextSlot.Equal(want) {
		t.Fatalf("fall next slot = %v, want %v", nextSlot, want)
	}
}

func TestNextSurvivesSpringForward(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	after := time.Date(2025, 3, 9, 1, 0, 0, 0, loc)

	type answer struct {
		next time.Time
		err  error
	}
	done := make(chan answer, 1)
	go func() {
		next, err := Next("0 2 * * *", after, loc)
		done <- answer{next: next, err: err}
	}()
	var got answer
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Next did not return across the spring-forward gap")
	}
	if got.err != nil {
		t.Fatalf("Next: %v", got.err)
	}
	want := time.Date(2025, 3, 9, 3, 0, 0, 0, loc)
	if !got.next.Equal(want) {
		t.Errorf("next = %v, want %v (first instant after the gap)", got.next, want)
	}
	if !got.next.After(after) {
		t.Errorf("next = %v, want strictly after %v", got.next, after)
	}
}

func TestNextFallBackDoesNotRepeatHour(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	after := time.Date(2025, 11, 1, 12, 0, 0, 0, loc)

	first, err := Next("30 1 * * *", after, loc)
	if err != nil {
		t.Fatalf("first Next: %v", err)
	}
	wantFirst := time.Date(2025, 11, 2, 1, 30, 0, 0, loc)
	if !first.Equal(wantFirst) {
		t.Fatalf("first = %v, want %v (EDT occurrence)", first, wantFirst)
	}
	second, err := Next("30 1 * * *", first, loc)
	if err != nil {
		t.Fatalf("second Next: %v", err)
	}
	wantSecond := time.Date(2025, 11, 3, 1, 30, 0, 0, loc)
	if !second.Equal(wantSecond) {
		t.Errorf("second = %v, want %v (no repeat of the 01:30 wall slot)", second, wantSecond)
	}
}

func TestNextSpringForwardDoesNotMissDay(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	after := time.Date(2025, 3, 8, 12, 0, 0, 0, loc)

	first, err := Next("30 2 * * *", after, loc)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2025, 3, 9, 3, 0, 0, 0, loc)
	if !first.Equal(want) {
		t.Fatalf("next = %v, want %v (gap day not missed)", first, want)
	}
	second, err := Next("30 2 * * *", first, loc)
	if err != nil {
		t.Fatalf("second Next: %v", err)
	}
	wantSecond := time.Date(2025, 3, 10, 2, 30, 0, 0, loc)
	if !second.Equal(wantSecond) {
		t.Errorf("second = %v, want %v", second, wantSecond)
	}
}

func TestNextStepsToNextTopOfHour(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	after := time.Date(2025, 3, 9, 1, 30, 0, 0, loc)
	got, err := Next("0 3 * * *", after, loc)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2025, 3, 9, 3, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("next = %v, want %v (same day, not skipped)", got, want)
	}
}

func TestNextRunFallBackRecoveryDoesNotDoubleFire(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	served := time.Date(2025, 11, 2, 5, 30, 0, 0, time.UTC).In(loc)
	now := time.Date(2025, 11, 2, 6, 0, 0, 0, time.UTC).In(loc)
	next, err := NextRun("30 1 * * *", served, now, loc)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want := time.Date(2025, 11, 3, 1, 30, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v (the repeated slot must not fire twice)", next, want)
	}
}

func TestNextWildcardHourSkipsRepeatedHour(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	after := time.Date(2025, 11, 2, 5, 59, 0, 0, time.UTC).In(loc)
	next, err := Next("* * * * *", after, loc)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2025, 11, 2, 7, 0, 0, 0, time.UTC).In(loc)
	if !next.Equal(want) {
		t.Errorf("next = %v, want %v (the second pass of 01:00-01:59 must be skipped)", next, want)
	}
}

func TestNextSpringForwardEastOfUTC(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	after := time.Date(2025, 3, 30, 1, 0, 0, 0, loc)
	next, err := Next("30 2 * * *", after, loc)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2025, 3, 30, 3, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Errorf("next = %v, want %v (the transition instant)", next, want)
	}
}
