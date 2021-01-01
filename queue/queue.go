package queue

import (
    "encoding/json"
    "errors"
    "io"
    "log"
    "strings"
    "sync"

    "golang.org/x/net/websocket"

    "../common/stopper"
    "../server/reply"
)

type void = struct{}

const maxDuration float64 = 30

type conn interface {
    //Stop()
    Deny(err error)
    SendOutput(stream string, output []byte) error
    SendResult(format string, contents []byte) error
    Complete(err error) error
}

type Task struct {
    stopper.Stopper
    queue *Queue
    conn  conn

    sources     map[string][]byte
    mainname    string
    duration    float64
    format      string
    stderrRedir bool
    verbosity   int

    started  bool
    backconn *websocket.Conn
    sendLock sync.Mutex
}

// XXX
// store task data
// when backend arrives, send data to it
// forward additional messages from conn to backend
// forward messages from backend to conn

// XXX
// states:
// • started = false, backconn = nil
// conn calls Start()
// • started = true , backconn = nil
// we notify queue, send current duration
// queue calls proceedWith(backend)
// we get backconn from backend
// • started = true , backconn

// XXX
// someone calls Stop()
// we die
// somehow we get out of queue
// somehow backconn closes

func newTask(conn conn) *Task {
    return &Task{
        conn:     conn,
        sources:  make(map[string][]byte),
        duration: maxDuration,
    Stopper: stopper.New(),
    }
}

func (task *Task) AddFile(filename string, contents []byte) error {
    // sync: server readloop
    // XXX impose total limit of 128kiB on input file size
    if task.started {
        return reply.Error("The task has already started, cannot add files")
    }
    task.sources[filename] = contents
    return nil
}

func (task *Task) SetDuration(duration float64) error {
    // sync: server readloop
    if duration < 0 || duration > maxDuration {
        duration = maxDuration
    }
    if task.started {
        task.queue.updates <- queueUpdate{
            task:     task,
            duration: duration,
        }
        return nil
    }
    task.duration = duration
    return nil
}

func (task *Task) SetFormat(format string) error {
    // sync: server readloop
    if task.started {
        return reply.Error("The task has already started, cannot set options")
    }
    task.format = format
    return nil
}

func (task *Task) SetStderrRedir(stderrRedir bool) error {
    // sync: server readloop
    if task.started {
        return reply.Error("The task has already started, cannot set options")
    }
    task.stderrRedir = stderrRedir
    return nil
}

func (task *Task) SetVerbosity(verbosity int) error {
    // sync: server readloop
    if task.started {
        return reply.Error("The task has already started, cannot set options")
    }
    task.verbosity = verbosity
    return nil
}

func (task *Task) Start(mainname string) error {
    // sync: server readloop
    if task.started {
        return reply.Error("The task has already started, cannot start again")
    }
    task.mainname = mainname
    task.started = true
    if task.duration == 0 {
        task.Stop()
        return nil
    }
    task.queue.updates <- queueUpdate{
        task:     task,
        start:    true,
        duration: task.duration,
    }
    return nil
}

//func (task *Task) proceedWith(backend *backend) {
//    // sync: queue loop XXX ? (see Dial)
//    backconn, err := backend.Dial(task)
//    if err != nil {
//        task.Stop()
//        return
//    }
//    task.backconn = backconn
//    go func() {
//        // sync: XXX (backend send)
//        if err := task.sendStart(); err != nil {
//            log.Print(err)
//            task.Stop()
//        }
//    }()
//    go task.receiveLoop()
//}

func (task *Task) sendStart() error {
    task.sendLock.Lock()
    defer task.sendLock.Unlock()
    // send files
    for filename, contents := range task.sources {
        var err error
        addArgsB, err := json.Marshal(struct {
            Filename string `json:"filename"`
        }{
            Filename: filename,
        })
        if err != nil {
            return err
        }
        addMsg := "add " + string(addArgsB)
        err = websocket.Message.Send(task.backconn, addMsg)
        if err != nil {
            return err
        }
        err = websocket.Message.Send(task.backconn, contents)
        if err != nil {
            return err
        }
    }
    { // send options
        var err error
        optionsArgsB, err := json.Marshal(struct {
            Duration    float64 `json:"duration"`
            Format      string  `json:"format"`
            StderrRedir bool    `json:"stderrRedir"`
            Verbosity   int     `json:"verbosity"`
        }{
            Duration:    task.duration,
            Format:      task.format,
            StderrRedir: task.stderrRedir,
            Verbosity:   task.verbosity,
        })
        if err != nil {
            return err
        }
        optionsMsg := "options " + string(optionsArgsB)
        err = websocket.Message.Send(task.backconn, optionsMsg)
        if err != nil {
            return err
        }
    }
    { // send start
        var err error
        startArgsB, err := json.Marshal(struct {
            Main string `json:"main"`
        }{
            Main: task.mainname,
        })
        if err != nil {
            return err
        }
        startMsg := "start " + string(startArgsB)
        err = websocket.Message.Send(task.backconn, startMsg)
        if err != nil {
            return err
        }
    }
    return nil
}

func (task *Task) sendDuration(duration float64) error {
    task.sendLock.Lock()
    defer task.sendLock.Unlock()
    var err error
    optionsArgsB, err := json.Marshal(struct {
        Duration float64 `json:"duration"`
    }{
        Duration: duration,
    })
    if err != nil {
        return err
    }
    optionsMsg := "options " + string(optionsArgsB)
    err = websocket.Message.Send(task.backconn, optionsMsg)
    if err != nil {
        return err
    }
    return nil
}

func (task *Task) receiveLoop() {
    defer task.Stop()

    const (
        resultPrefix   = "result "
        outputPrefix   = "output "
        completePrefix = "complete "
        denyPrefix     = "deny "
    )

    for {
        var message string
        err := websocket.Message.Receive(task.backconn, &message)
        if err != nil {
            if errors.Is(err, io.EOF) {
                return
            }
            select {
            case <-task.Stopped:
            default:
                log.Println("backend websocket receive:", err)
            }
            return
        }
        switch {
        case strings.HasPrefix(message, outputPrefix):
            var outputArgs struct {
                Stream string
            }
            if err := json.Unmarshal(
                []byte(message[len(outputPrefix):]), &outputArgs,
            ); err != nil {
                log.Println("'output' arguments are not a correct JSON:", err)
                return
            }
            var contents []byte
            err = websocket.Message.Receive(task.backconn, &contents)
            if err != nil {
                log.Print(err)
                return
            }
            if err := task.conn.SendOutput(outputArgs.Stream, contents); err != nil {
                log.Print(err)
                return
            }
        case strings.HasPrefix(message, resultPrefix):
            var resultArgs struct {
                Format string
            }
            if err := json.Unmarshal(
                []byte(message[len(resultPrefix):]), &resultArgs,
            ); err != nil {
                log.Println("'result' arguments are not a correct JSON:", err)
                return
            }
            var contents []byte
            err = websocket.Message.Receive(task.backconn, &contents)
            if err != nil {
                log.Print(err)
                return
            }
            if err := task.conn.SendResult(resultArgs.Format, contents); err != nil {
                log.Print(err)
                return
            }
        case strings.HasPrefix(message, completePrefix):
            var completeArgs struct {
                Error *string
            }
            if err := json.Unmarshal(
                []byte(message[len(completePrefix):]), &completeArgs,
            ); err != nil {
                log.Println("'complete' arguments are not a correct JSON:", err)
                return
            }
            var completeErr error = nil
            if completeArgs.Error != nil {
                completeErr = reply.Error(*completeArgs.Error)
            }
            if err := task.conn.Complete(completeErr); err != nil {
                log.Print(err)
                return
            }
            return
        case strings.HasPrefix(message, denyPrefix):
            var denyArgs struct {
                Error string
            }
            if err := json.Unmarshal(
                []byte(message[len(denyPrefix):]), &denyArgs,
            ); err != nil {
                log.Println("'deny' arguments are not a correct JSON:", err)
                return
            }
            task.conn.Deny(reply.Error(denyArgs.Error))
            return
        default:
            log.Print("backend websocket receive: unknown command")
            return
        }
    }
}

type backend struct {
    queue *Queue
    addr  string
}

// create and return websocket connection
// monitor the respective task and close the connection when the task stops
// return backend to the queue pool when connection is closed
func (b *backend) Dial() (*websocket.Conn, error) {
    // sync: queue loop
    // XXX debug: specify wrong protocol
    conn, err := websocket.Dial("ws://"+b.addr+"/asy", "asyonline/asy", "http://localhost/asy")
    if err != nil {
        return nil, err
    }
    return conn, nil
}


// forward-linked list, supplemented by item map
type queueList struct {
    // first of the slices must exist, pointing to the actual start of the list
    slices []queueListSlice
    last   *queueListItem
    items  map[*Task]*queueListItem
}

type queueListSlice = struct {
    // must not exceed maxDuration
    duration float64
    first    *queueListItem
}

type queueListItem struct {
    task     *Task
    duration float64
    position uint
    next     *queueListItem
}

func newQueueList() *queueList {
    placeholder := &queueListItem{}
    return &queueList{
        slices: []queueListSlice{{maxDuration, placeholder}},
        last:   placeholder,
        items:  make(map[*Task]*queueListItem),
    }
}

// find the first element in list with at most such duration
func (q *queueList) first(duration float64) *queueListItem {

    // find or create the slice
    var (
        i     int = -1
        slice *queueListSlice
    )
    for ii, sslice := range q.slices {
        if sslice.duration > duration {
            continue
        }
        if sslice.duration == duration {
            i, slice = ii, &q.slices[ii]
            break
        }
        if ii == 0 {
            i, slice, duration = ii, &q.slices[ii], maxDuration
            break
        }
        q.slices = append(q.slices[:ii], q.slices[ii-1:]...)
        q.slices[ii].duration = duration
        i, slice = ii, &q.slices[ii]
    }
    if i < 0 {
        q.slices = append(q.slices, q.slices[len(q.slices)-1])
        i, slice = len(q.slices)-1, &q.slices[len(q.slices)-1]
        slice.duration = duration
    }

    // find the element
    var first *queueListItem = slice.first
    for first != nil && (first.task == nil || first.duration > duration) {
        first = first.next
    }
    if first != nil {
        slice.first = first
    } else {
        slice.first = q.last
    }

    { // clean outpaced slices
        var ii int
        var size uint = q.last.position - q.slices[0].first.position + 1
        for ii = i + 1; ii < len(q.slices); ii++ {
            if (slice.first.position - q.slices[ii].first.position) < size {
                break
            }
        }
        if ii > i+1 {
            q.slices = append(q.slices[:i+1], q.slices[ii:]...)
        }
    }

    return first
}

func (qi *queueListItem) getNext() *queueListItem {
    var next *queueListItem = qi.next
    for next != nil && next.task == nil {
        next = next.next
    }
    return next
}

func (q *queueList) find(task *Task) *queueListItem {
    if item, ok := q.items[task]; ok {
        return item
    }
    return nil
}

func (q *queueList) add(task *Task, duration float64) {
    if task == nil {
        panic("nil task")
    }
    if _, ok := q.items[task]; ok {
        panic("task is already here")
        // XXX maybe replace duration, if task is already here?
    }
    if !(duration > 0) {
        panic("duration must be positive")
    }
    if duration > maxDuration {
        duration = maxDuration
    }
    item := &queueListItem{
        task:     task,
        duration: duration,
        position: q.last.position + 1,
        next:     nil,
    }
    q.items[task] = item
    q.last.next = item
    q.last = item
    return
}

func (q *queueList) remove(task *Task) {
    item, ok := q.items[task]
    if !ok {
        panic("there is no such task")
    }
    delete(q.items, task)
    item.task = nil
    return
}

// Queue
// requirements:
// • add to queue
// • iteration in queue order
// • find first element that satisfies certain condition
// • update/remove arbitrary element by value

// Pile of currently-executing tasks
// requirements:
// • add to the pile
// • maintain statistics of durations
// • update duration of specific task

// hold a pile of tasks
// hold a pile of backend connectors
// assign a task to a free backend
// maintain limits on tasks with large durations

// create a task
// when task.Run is called, add the task to some queue structure, respecting its duration
// if task duration is updated, we need to know it
// if the task is closed, remove it

// when there is a free backend, assign it to the task in front of the queue
// when task stops, accept backend back in free pool

type queueUpdate = struct {
    task     *Task
    start    bool
    duration float64
}

type Queue struct {
    list     *queueList
    updates chan<- queueUpdate
    backends chan<- *backend
}

func NewQueue() *Queue {
    updates := make(chan queueUpdate)
    backends := make(chan *backend)
    queue := &Queue{
        list: newQueueList(),
        updates: updates,
        backends: backends,
    }
    // XXX init backends maybe
    go queue.loop(updates, backends)
    return queue
}

func (queue *Queue) NewTask(conn conn) (*Task, error) {
    task := newTask(conn)
    task.queue = queue
    return task, nil
}

func (queue *Queue) AddBackend(addr string) {
    b := &backend{queue, addr}
    go func() {
        queue.backends <- b
    }()
}

func (queue *Queue) loop(updates <-chan queueUpdate, backends <-chan *backend) {
    // we only select backends if we need them
    var backendsMaybe <-chan *backend
    for {
        if len(queue.list.items) > 0 {
            backendsMaybe = backends
        } else {
            backendsMaybe = nil
        }
        select {
        case update := <-updates:
            task := update.task
            if update.start {
                queue.list.add(task, task.duration)
                break
            }
            if task.duration <= update.duration {
                break
            }
            if task.backconn != nil {
                task.sendDuration(update.duration)
            }
            task.duration = update.duration
        case b := <-backendsMaybe:
            queueItem := queue.list.first(maxDuration)
            if queueItem == nil {
                panic("impossible")
            }
            task := queueItem.task
            queue.list.remove(task)
            backconn, err := b.Dial() // XXX may block?
            if err != nil {
                log.Print(err)
                task.Stop()
                go func(b *backend) {
                    queue.backends <- b
                }(b)
                return
            }
            task.backconn = backconn
            go func(b *backend, backconn *websocket.Conn, task *Task) {
                defer func() {
                    queue.backends <- b // may block
                }()
                defer backconn.Close()
                <-task.Stopped
            }(b, backconn, task)
            go func(task *Task) {
                if err := task.sendStart(); err != nil {
                    log.Print(err)
                    task.Stop()
                }
            }(task)
            go task.receiveLoop()
        }
    }
}

func (queue *Queue) setDuration(task *Task, duration float64) {
    // sync: server readloop
    // XXX actually, first we need to check if the task is already executing
    item := queue.list.find(task)
    if item == nil {
        // XXX add task to the queue
        return
    }
    // XXX determine the actual duration allowed
    queue.list.add(task, duration)
    // XXX there are two durations
    // one is nominal requested by client
    // another is real, assigned by queue
    if task.backconn != nil { // XXX sync with proceedWith
        task.sendDuration(duration)
    }
}

// TODO Further plans
// Factor out of Queue:
// • Dispatcher that would match tasks from queue with backends if the limits are met
