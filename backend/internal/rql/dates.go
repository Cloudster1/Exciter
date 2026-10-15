package rql

import (
	"strconv"
	"strings"
	"time"
)

// dateFunctions are the moments a query can name relative to now.
var dateFunctions = []string{"now", "startOfDay", "endOfDay", "startOfWeek", "endOfWeek", "startOfMonth", "endOfMonth"}

// instant resolves a date value. wholeDay says the value named a day with no
// time, which the timestamp comparison turns into a range.
func (c *compiler) instant(v Value) (time.Time, bool, error) {
	loc := c.env.Location
	switch v.Kind {
	case VDate:
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
			if at, err := time.ParseInLocation(layout, v.Text, loc); err == nil {
				return at, false, nil
			}
		}
		at, err := time.ParseInLocation("2006-01-02", v.Text, loc)
		if err != nil {
			return time.Time{}, false, errAt(v.Pos, "%q is not a calendar date. Write it like 2026-09-04.", v.Text)
		}
		return at, true, nil
	case VDuration:
		d, err := duration(v)
		if err != nil {
			return time.Time{}, false, err
		}
		return c.env.Now.In(loc).Add(d), false, nil
	case VFunction:
		ref := c.env.Now.In(loc)
		if v.Arg != nil {
			d, err := duration(*v.Arg)
			if err != nil {
				return time.Time{}, false, err
			}
			ref = ref.Add(d)
		}
		y, m, d := ref.Date()
		day := time.Date(y, m, d, 0, 0, 0, 0, loc)
		// Weeks start on Monday, as the plan's weeks do.
		weekday := (int(day.Weekday()) + 6) % 7
		week := day.AddDate(0, 0, -weekday)
		month := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		switch strings.ToLower(v.Text) {
		case "now":
			return ref, false, nil
		case "startofday":
			return day, false, nil
		case "endofday":
			return day.AddDate(0, 0, 1).Add(-time.Second), false, nil
		case "startofweek":
			return week, false, nil
		case "endofweek":
			return week.AddDate(0, 0, 7).Add(-time.Second), false, nil
		case "startofmonth":
			return month, false, nil
		case "endofmonth":
			return month.AddDate(0, 1, 0).Add(-time.Second), false, nil
		}
		return time.Time{}, false, errAt(v.Pos, "There is no function %s(). The date functions are %s.", v.Text, describeOperators(withParens(dateFunctions)))
	}
	return time.Time{}, false, errAt(v.Pos, "%s is not a date. Write 2026-09-04, -7d or startOfWeek().", v)
}

// duration reads -7d, 2w or 36h. A day is 24 hours here; a query does not
// care about the hour a clock changed.
func duration(v Value) (time.Duration, error) {
	text := v.Text
	unit := text[len(text)-1]
	n, err := strconv.Atoi(text[:len(text)-1])
	if err != nil {
		return 0, errAt(v.Pos, "%q is not a duration. Write it like -7d, 2w or 36h.", text)
	}
	switch unit {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	default:
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
}
