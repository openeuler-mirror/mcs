package timex

import "time"

type Clock func() time.Time

func Now(clock Clock) time.Time {
	if clock != nil {
		return clock()
	}
	return time.Now()
}
