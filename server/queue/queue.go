package queue

const maxDuration float64 = 30

type Queue struct {
    list     *taskList
    backends *backendPool
}

func NewQueue(addrs []string) *Queue {
    list := newTaskList(maxDuration)
    backends := newBackendPool()
    for _, addr := range addrs {
        go func(addr string) {
            backends.input <- &backend{addr}
        }(addr)
    }
    queue := &Queue{
        list:     list,
        backends: backends,
    }
    go dispatchLoop(list, backends, []QueueLevel{{maxDuration, len(addrs)}})
    return queue
}

func (queue *Queue) NewTask(conn conn) (*Task, error) {
    task := newTask(conn)
    task.queue = queue
    return task, nil
}

type QueueLevel = struct {
    duration float64
    limit    int
}

