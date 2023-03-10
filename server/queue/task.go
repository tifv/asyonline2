package queue

import (
    "encoding/json"
    "errors"
    "io"
    "log"
    "strings"

    "golang.org/x/net/websocket"

    "asyonline/server/common/stopper"
    "asyonline/server/server/reply"
)

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

    // these must not change after started becomes true
    sources     map[string][]byte
    mainname    string
    duration    float64
    format      string
    stderrRedir bool
    verbosity   int

    started   bool
    backconn  *websocket.Conn
    durations chan<- float64
    backends  chan<- *backend
}

func newTask(conn conn) *Task {
    return &Task{
        conn:     conn,
        sources:  make(map[string][]byte),
        duration: maxDuration,
        Stopper:  stopper.New(),
    }
}

func (t *Task) AddFile(filename string, contents []byte) error {
    // sync: server readloop
    // XXX impose total limit of 128kiB on input file size
    if t.started {
        return reply.Error("The task has already started, cannot add files")
    }
    t.sources[filename] = contents
    return nil
}

func (t *Task) SetDuration(duration float64) error {
    // sync: server readloop
    if duration < 0 || duration > maxDuration {
        duration = maxDuration
    }
    if t.started {
        select {
        case t.durations <- duration:
        case <-t.Stopped:
        }
        return nil
    }
    t.duration = duration
    return nil
}

func (t *Task) SetFormat(format string) error {
    // sync: server readloop
    if t.started {
        return reply.Error("The task has already started, cannot set options")
    }
    t.format = format
    return nil
}

func (t *Task) SetStderrRedir(stderrRedir bool) error {
    // sync: server readloop
    if t.started {
        return reply.Error("The task has already started, cannot set options")
    }
    t.stderrRedir = stderrRedir
    return nil
}

func (t *Task) SetVerbosity(verbosity int) error {
    // sync: server readloop
    if t.started {
        return reply.Error("The task has already started, cannot set options")
    }
    t.verbosity = verbosity
    return nil
}

func (t *Task) Start(mainname string) error {
    // sync: server readloop
    if t.started {
        return reply.Error("The t has already started, cannot start again")
    }
    t.mainname = mainname
    var durations = make(chan float64)
    t.durations = durations
    t.started = true
    if t.duration == 0 {
        t.Stop()
        return nil
    }
    go t.loop(durations)
    return nil
}

func (t *Task) proceedWith(b *backend) {
    t.backends <- b
    close(t.backends)
}

func (t *Task) loop(durations <-chan float64) {
    defer t.Stop()
    var duration float64 = t.duration
    var backends <-chan *backend
    {
        var backendsRS = make(chan *backend)
        backends = backendsRS
        t.backends = backendsRS
    }
    select {
    case t.queue.list.input <- t:
    case <-t.Stopped:
        return
    }

    var b *backend
    for {
        select {
        case newDuration := <-durations:
            if newDuration < duration {
                duration = newDuration
            }
            continue
        case b = <-backends:
        case <-t.Stopped:
            return
        }
        break
    }

    var err error
    backconn, err := b.Dial()
    if err != nil {
        log.Print(err)
        return
    }
    defer backconn.Close()
    t.backconn = backconn
    go t.receiveLoop()
    err = t.sendStart(duration)
    if err != nil {
        log.Print(err)
        return
    }
    for {
        select {
        case newDuration := <-durations:
            if newDuration < duration {
                duration = newDuration
            }
            err := t.sendDuration(duration)
            if err != nil {
                log.Print(err)
                return
            }
        case <-t.Stopped:
            return
        }
    }
}

func (task *Task) sendStart(duration float64) error {
    // sync: task loop
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
            Duration:    duration,
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
    // sync: task loop
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
            var err error
            var outputArgs struct {
                Stream string
            }
            err = json.Unmarshal(
                []byte(message[len(outputPrefix):]), &outputArgs)
            if err != nil {
                log.Println("'output' arguments are not a correct JSON:", err)
                return
            }
            var contents []byte
            err = websocket.Message.Receive(task.backconn, &contents)
            if err != nil {
                log.Print(err)
                return
            }
            err = task.conn.SendOutput(outputArgs.Stream, contents)
            if err != nil {
                log.Print(err)
                return
            }
        case strings.HasPrefix(message, resultPrefix):
            var err error
            var resultArgs struct {
                Format string
            }
            err = json.Unmarshal(
                []byte(message[len(resultPrefix):]), &resultArgs)
            if err != nil {
                log.Println("'result' arguments are not a correct JSON:", err)
                return
            }
            var contents []byte
            err = websocket.Message.Receive(task.backconn, &contents)
            if err != nil {
                log.Print(err)
                return
            }
            err = task.conn.SendResult(resultArgs.Format, contents)
            if err != nil {
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
