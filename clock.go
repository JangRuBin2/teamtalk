package main

import (
	"fmt"
	"os"
	"time"
)

// now returns the current time. TEAMTALK_NOW ("2006-01-02 15:04") overrides it for testing.
func now() time.Time {
	if v := os.Getenv("TEAMTALK_NOW"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04", v, time.Local); err == nil {
			return t
		}
	}
	return time.Now()
}

// parseHM parses "HH:MM" into minutes since midnight.
func parseHM(s string) (int, bool) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func atMinutes(day time.Time, min int) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, min/60, min%60, 0, 0, day.Location())
}

// workDay returns today's clock-in and clock-out times from the config.
func workDay(t time.Time) (on, off time.Time) {
	s, _ := parseHM(cfg.WorkStart)
	e, _ := parseHM(cfg.WorkEnd)
	return atMinutes(t, s), atMinutes(t, e)
}

func fmtClock(t time.Time) string { return t.Format("15:04") }

func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := int(d.Minutes())
	if m < 60 {
		return fmt.Sprintf("%d분", m)
	}
	return fmt.Sprintf("%d시간 %02d분", m/60, m%60)
}

// workStatus is the one-line clock shown in every game header.
func workStatus() string {
	t := now()
	on, off := workDay(t)
	switch {
	case t.Before(on):
		return fmt.Sprintf("%s  출근 전 (출근까지 %s)", fmtClock(t), fmtDur(on.Sub(t)))
	case t.Before(off):
		return fmt.Sprintf("%s  퇴근까지 %s", fmtClock(t), fmtDur(off.Sub(t)))
	default:
		return fmt.Sprintf("%s  퇴근 시간 지남 (+%s)", fmtClock(t), fmtDur(t.Sub(off)))
	}
}
