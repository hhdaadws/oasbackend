package taskmeta

import "time"

// BJLoc is Beijing timezone (UTC+8), exported for use by other packages.
var BJLoc = time.FixedZone("Asia/Shanghai", 8*60*60)

// Alias for internal use.
var bjLoc = BJLoc

// GetNextTimeRule returns the next_time_rule for a given task name.
// Returns empty string if the task is unknown.
func GetNextTimeRule(taskName string) string {
	cfg, ok := defaultTaskConfig[taskName]
	if !ok {
		return ""
	}
	rule, _ := cfg["next_time_rule"].(string)
	return rule
}

// CalcNextTime computes the next execution time based on rule and current time.
// Returns the result in Beijing timezone for human-readable storage.
// Returns zero time for "on_demand" or unknown rules.
func CalcNextTime(rule string, now time.Time) time.Time {
	bj := now.In(bjLoc)

	switch rule {
	case "daily_reset":
		// Tomorrow Beijing time 00:01
		tomorrow := time.Date(bj.Year(), bj.Month(), bj.Day()+1, 0, 1, 0, 0, bjLoc)
		return tomorrow

	case "weekly_monday":
		// Next Monday Beijing time 00:01
		daysUntilMonday := (8 - int(bj.Weekday())) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7
		}
		nextMon := time.Date(bj.Year(), bj.Month(), bj.Day()+daysUntilMonday, 0, 1, 0, 0, bjLoc)
		return nextMon

	case "interval_6h":
		return now.Add(6 * time.Hour).In(bjLoc)

	case "interval_8h":
		return now.Add(8 * time.Hour).In(bjLoc)

	case "interval_2h_window":
		// 根据当前北京时间的窗口推算下一个窗口（对弈竞猜窗口: 10,12,14,16,18,20,22）
		currentWindowHour := (bj.Hour() / 2) * 2
		nextWindowHour := currentWindowHour + 2

		if nextWindowHour > 22 || nextWindowHour < 10 {
			// 超出有效窗口范围，推到次日 10:00 BJ
			nextDay := time.Date(bj.Year(), bj.Month(), bj.Day()+1, 10, 0, 0, 0, bjLoc)
			return nextDay
		}
		return time.Date(bj.Year(), bj.Month(), bj.Day(), nextWindowHour, 0, 0, 0, bjLoc)

	case "coop_window":
		// Next Beijing time 18:00 or 21:00
		today18 := time.Date(bj.Year(), bj.Month(), bj.Day(), 18, 0, 0, 0, bjLoc)
		today21 := time.Date(bj.Year(), bj.Month(), bj.Day(), 21, 0, 0, 0, bjLoc)
		if now.Before(today18) {
			return today18
		}
		if now.Before(today21) {
			return today21
		}
		// Next day 18:00
		tomorrow18 := time.Date(bj.Year(), bj.Month(), bj.Day()+1, 18, 0, 0, 0, bjLoc)
		return tomorrow18

	case "weekly_7d":
		return now.Add(7 * 24 * time.Hour).In(bjLoc)

	case "on_demand":
		return time.Time{}

	default:
		return time.Time{}
	}
}
