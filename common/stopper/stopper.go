package stopper

import (
    "sync"
)

type void = struct{}

type Stopper struct {
    Stopped  <-chan void
    stopFunc func()
}

func New() Stopper {
    var stopped = make(chan void)
    var once sync.Once
    var stop = func() { close(stopped) }
    return Stopper{
        Stopped:  stopped,
        stopFunc: func() { once.Do(stop) },
    }
}

func (s Stopper) Stop() {
    s.stopFunc()
}

