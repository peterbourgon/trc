package trc

import (
	"fmt"
	"strings"
	"time"
)

type Stopwatch struct {
	begin time.Time
	last  time.Time
	laps  map[string]lap
	order []string
}

type lap struct {
	n int
	d time.Duration
}

func NewStopwatch() *Stopwatch {
	now := time.Now()
	return &Stopwatch{
		begin: now,
		last:  now,
		laps:  map[string]lap{},
	}
}

func (s *Stopwatch) Lap(name string) time.Duration {
	now := time.Now()
	took := now.Sub(s.last)
	s.last = now
	lap, ok := s.laps[name]
	if !ok {
		s.order = append(s.order, name)
	}
	lap.n += 1
	lap.d += took
	s.laps[name] = lap
	return took
}

func (s *Stopwatch) Report(name string) string {
	switch lap := s.laps[name]; lap.n {
	case 0:
		return fmt.Sprintf("%s n/a", name)
	case 1:
		return fmt.Sprintf("%s %s", name, humanDuration(lap.d))
	default:
		return fmt.Sprintf("%s %dx%s=%s", name, lap.n, humanDuration(lap.d/time.Duration(lap.n)), humanDuration(lap.d))
	}
}

func (s *Stopwatch) Overall() time.Duration {
	return time.Since(s.begin)
}

func (s *Stopwatch) String() string {
	overall := fmt.Sprintf("overall %s", humanDuration(time.Since(s.begin)))

	if len(s.order) <= 0 {
		return overall
	}

	var (
		cum  time.Duration
		laps []string
	)
	for _, name := range s.order {
		cum += s.laps[name].d
		laps = append(laps, s.Report(name))
	}

	sums := []string{
		fmt.Sprintf("cumulative %s", humanDuration(cum)),
		overall,
	}

	var (
		lapTimes = strings.Join(laps, ", ")
		sumTimes = strings.Join(sums, ", ")
		allTimes = strings.Join([]string{lapTimes, sumTimes}, "; ")
	)
	return allTimes
}

//
//
//

type humanDuration time.Duration

func (hd humanDuration) humanize() time.Duration {
	switch d := time.Duration(hd); true {
	case d > time.Second:
		return d.Truncate(100 * time.Millisecond)
	case d > time.Millisecond:
		return d.Truncate(100 * time.Microsecond)
	case d > time.Microsecond:
		return d.Truncate(100 * time.Nanosecond)
	default:
		return d
	}
}

func (hd humanDuration) String() string {
	return hd.humanize().String()
}
