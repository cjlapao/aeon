package automationview

import (
	"fmt"
	"strconv"
	"strings"
)

// describe.go — the small, EXHAUSTIVE table for common cron shapes. Any
// expression the table does not cover prints raw inside `cron "…"` — a
// paraphrase nobody verified is worse than the expression itself, so the
// table only holds shapes whose words are mechanically true, each one a
// test row.

// cronFields splits and sanity-checks the five fields; anything unexpected
// (names beyond days, six-field forms, @macro lines) is not table material.
func cronFields(expr string) ([]string, bool) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, false
	}
	return f, true
}

var dayNames = map[string]string{
	"0": "Sunday", "1": "Monday", "2": "Tuesday", "3": "Wednesday",
	"4": "Thursday", "5": "Friday", "6": "Saturday", "7": "Sunday",
	"sun": "Sunday", "mon": "Monday", "tue": "Tuesday", "wed": "Wednesday",
	"thu": "Thursday", "fri": "Friday", "sat": "Saturday",
}

// cronWords renders the expression, or falls back to the raw print.
func cronWords(expr string) string {
	f, ok := cronFields(expr)
	if !ok {
		return rawCron(expr)
	}
	mi, hi, dom, mon, dow := f[0], f[1], f[2], f[3], f[4]
	// "every N minutes" family.
	if strings.HasPrefix(mi, "*/") && hi == "*" && dom == "*" && mon == "*" && dow == "*" {
		if n, err := strconv.Atoi(mi[2:]); err == nil && n > 0 {
			return plural(n, "every minute", "every %d minutes")
		}
		return rawCron(expr)
	}
	if isInt(mi) && strings.HasPrefix(hi, "*/") && dom == "*" && mon == "*" && dow == "*" {
		if n, err := strconv.Atoi(hi[2:]); err == nil && n > 0 {
			return fmt.Sprintf("every %d hours at minute %s", n, mi)
		}
		return rawCron(expr)
	}
	if !isInt(mi) || !isInt(hi) {
		return rawCron(expr)
	}
	clock := fmt.Sprintf("%02d:%02d", must(hi), must(mi))
	switch {
	case dom == "*" && mon == "*" && dow == "*":
		return "every day at " + clock
	case dom == "*" && mon == "*" && dow == "1-5":
		return "weekdays at " + clock
	case dom == "*" && mon == "*" && isDay(dow):
		return "every " + dayWord(dow) + " at " + clock
	case dom == "*" && mon == "*" && dow == "0,6":
		return "weekends at " + clock
	case dom == "*" && mon == "*" && strings.HasPrefix(dow, "*/"):
		if n, err := strconv.Atoi(dow[2:]); err == nil && n > 0 {
			return fmt.Sprintf("every %d days of the week at %s", n, clock)
		}
		return rawCron(expr)
	case isInt(dom) && mon == "*" && dow == "*":
		return fmt.Sprintf("day %s of every month at %s", dom, clock)
	case dow == "*" && strings.HasPrefix(dom, "*/") && mon == "*":
		if n, err := strconv.Atoi(dom[2:]); err == nil && n > 0 {
			return fmt.Sprintf("every %d days of the month at %s", n, clock)
		}
		return rawCron(expr)
	}
	return rawCron(expr)
}

func rawCron(expr string) string {
	return fmt.Sprintf("cron %q", expr)
}

func isInt(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func must(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func isDay(s string) bool {
	_, ok := dayNames[strings.ToLower(s)]
	return ok
}

func dayWord(s string) string {
	if w, ok := dayNames[strings.ToLower(s)]; ok {
		return w
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return fmt.Sprintf(many, n)
}

// windowWords renders "Mon–Fri 09:00–18:00" from the row's fields; the empty
// window prints nothing (the schedule has no working-hours clause).
func windowWords(start, end, days string) string {
	if start == "" && end == "" {
		return ""
	}
	dl := daysWords(days)
	cl := start + "–" + end
	if dl == "" {
		return "every day " + cl
	}
	return dl + " " + cl
}

var shortDays = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// daysWords renders the ISO day list (1=Mon…7=Sun) with an en-dash for
// consecutive runs: "1,2,3,4,5" → "Mon–Fri"; gaps list with commas.
func daysWords(days string) string {
	if days == "" {
		return ""
	}
	var on []int
	for _, p := range strings.Split(days, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 1 || n > 7 {
			return "days " + days // unrenderable: print the data, not a guess
		}
		on = append(on, n)
	}
	var parts []string
	for i := 0; i < len(on); {
		j := i
		for j+1 < len(on) && on[j+1] == on[j]+1 {
			j++
		}
		if j-i >= 2 {
			parts = append(parts, shortDays[on[i]-1]+"–"+shortDays[on[j]-1])
		} else {
			for k := i; k <= j; k++ {
				parts = append(parts, shortDays[on[k]-1])
			}
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}
