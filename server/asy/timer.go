package asy

import (
    "time"
)

type timer struct {
    durations chan<- time.Duration
    start     chan<- void
    end       <-chan void

    // only timer loop can access this before end
    duration time.Duration
}

func newTimer(stopped <-chan void) *timer {
    durations := make(chan time.Duration)
    start := make(chan void)
    end := make(chan void)
    timer := &timer{durations, start, end, -1}
    go timer.loop(durations, start, end, stopped)
    return timer
}

func (timer *timer) setDuration(duration time.Duration) {
    select {
    case timer.durations <- duration:
    case <-timer.end:
    }
}

func (timer *timer) loop(
    durations <-chan time.Duration, start <-chan void, end chan<- void,
    stopped <-chan void,
) {
    defer close(end)
    var st time.Time
    var endish <-chan void
    var makeEndish = func() <-chan void {
        endish := make(chan void)
        go func(et time.Time, endish chan<- void) {
            time.Sleep(time.Until(et))
            close(endish)
        }(st.Add(timer.duration), endish)
        return endish
    }
    for {
        select {
        case duration := <-durations:
            if duration < 0 {
                panic("timer duration must not be negative")
            }
            if duration == 0 {
                timer.duration = 0
                return
            }
            if timer.duration < 0 || duration < timer.duration {
                timer.duration = duration
                if !st.IsZero() {
                    endish = makeEndish()
                }
            }
        case <-start:
            start = nil
            st = time.Now()
            if timer.duration >= 0 {
                endish = makeEndish()
            }
        case <-stopped:
            return
        case <-endish:
            return
        }
    }
}

