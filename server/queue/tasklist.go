package queue

type taskList struct {
    input   chan<- *Task
    filters chan<- float64
    output  <-chan *Task
    // input and filters channels will block if output is clogged
}

func newTaskList(maxDuration float64) *taskList {
    var (
        input   = make(chan *Task)
        filters = make(chan float64)
        output  = make(chan *Task)
    )
    go func(
        input <-chan *Task,
        filters <-chan float64,
        output chan<- *Task,
    ) {
        defer close(output)
        type (
            taskListItem = struct {
                task     *Task
                duration float64
            }
            taskListSlice = struct {
                duration float64
                offset   int
            }
        )
        var (
            tasks          = []taskListItem{}
            offset int    = 0
            slices         = []taskListSlice{{maxDuration, 0}}
            i      int     = len(slices)
            filter float64 = 0.0
        )
        for {
            select {
            case task, ok := <-input:
                if !ok {
                    return
                }
                duration := task.duration
                if duration <= 0 || duration > maxDuration {
                    duration = maxDuration
                }
                tasks = append(tasks, taskListItem{task, duration})
            case filter = <-filters:
                for i > 0 && slices[i-1].duration <= filter {
                    i--
                }
            }
            if filter == 0.0 || i == len(slices) {
                continue
            }
            if slices[i].duration < filter && i > 0 {
                slices = append(slices[:i], slices[i-1:]...)
                slices[i].duration = filter
            }
            slice := &slices[i]
            var found bool = false
            for (slice.offset - offset) < len(tasks) {
                item := &tasks[slice.offset-offset]
                if item.task == nil || i > 0 && item.duration > slice.duration {
                    slice.offset++
                } else if found {
                    break
                } else {
                    output <- item.task
                    item.task = nil
                    found = true
                }
            }

            //cleanup_slices:
            {
                var ii int = i + 1
                for ii < len(slices) && (slices[ii].offset-slice.offset) < 0 {
                    ii++
                }
                if ii > i+1 {
                    slices = append(slices[:i+1], slices[ii:]...)
                }
            }

            //cleanup_tasks:
            if i == 0 && (slice.offset-offset) > 0 {
                tasks = tasks[slice.offset-offset:]
                offset = slice.offset
            }

            if found {
                i, filter = len(slices), 0.0
            }
        }
    }(input, filters, output)
    return &taskList{
        input:   input,
        filters: filters,
        output:  output,
    }
}

