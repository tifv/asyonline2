package queue

type void = struct{}

func dispatchLoop(
    list *taskList, backends *backendPool,
    levels []QueueLevel,
) {
    // TODO add mechanism for shutting the loop down
    // (or updating with different parameters)
    type gateItem = struct {
        duration float64
        gate     chan void
    }
    gates := make([]gateItem, len(levels)+1)
    // last gate is a sentinel
    for i, level := range levels {
        gate := make(chan void, level.limit)
        for j := 0; j < level.limit; j++ {
            gate <- void{}
        }
        gates[i] = gateItem{level.duration, gate}
    }
    var (
        level    int     = 0
        duration float64 = 0
    )
    for {
        select {
        case <-gates[level].gate:
            duration = gates[level].duration
            level++
            continue
        default:
        }
        var t *Task = nil
        select {
        case list.filters <- duration:
            select {
            case <-gates[level].gate:
                duration = gates[level].duration
                level++
                continue
            case t = <-list.output:
            }
        case t = <-list.output:
        }
        b := <-backends.output
        t.proceedWith(b)
        for _, gi := range gates[:level] {
            go func(t *Task, gi gateItem) {
                if t.duration > gi.duration {
                    <-t.Stopped
                }
                gi.gate <- void{}
            }(t, gi)
        }
        go func(t *Task, b *backend) {
            <-t.Stopped
            backends.input <- b
        }(t, b)
    }
}

