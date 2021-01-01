package server

import (
    "encoding/json"
    "errors"
    "io"
    "log"
    "strings"

    "golang.org/x/net/websocket"

    "../common/stopper"
    "../server/reply"
)

type void = struct{}

type task interface {
    AddFile(filename string, contents []byte) error
    SetDuration(duration float64) error
    SetFormat(format string) error
    SetStderrRedir(stderrRedir bool) error
    SetVerbosity(verbosity int) error
    Start(mainname string) error
    Stop()
}

type cache interface {
}

type Conn struct {
    stopper.Stopper
    ws    *websocket.Conn
    task  task
    cache cache
}

func NewConn(ws *websocket.Conn, cache cache) *Conn {
    conn := &Conn{
        ws:      ws,
        cache:   cache,
        Stopper: stopper.New(),
    }
    return conn
}

// XXX we should synchronize all sending methods because Deny may be executed concurrently with e.g. SendResult

func (conn *Conn) Deny(e error) {
    var err error
    var denyArgs = struct {
        Error string `json:"error"`
    }{}
    switch e := e.(type) {
    case reply.Error:
        denyArgs.Error = e.Error()
    default:
        log.Print(e)
        denyArgs.Error = "Server error"
    }
    denyArgsB, err := json.Marshal(denyArgs)
    if err != nil {
        log.Print(err)
        return
    }
    denyMsg := "deny " + string(denyArgsB)
    err = websocket.Message.Send(conn.ws, denyMsg)
    if err != nil {
        log.Print(err)
        return
    }
}

func (conn *Conn) SendOutput(name string, output []byte) error {
    var err error
    var outputArgs = struct {
        Stream string `json:"stream"`
    }{
        Stream: name,
    }
    outputArgsB, err := json.Marshal(outputArgs)
    if err != nil {
        return err
    }
    outputMsg := "output " + string(outputArgsB)
    err = websocket.Message.Send(conn.ws, outputMsg)
    if err != nil {
        return err
    }
    err = websocket.Message.Send(conn.ws, output)
    if err != nil {
        return err
    }
    return nil
}

func (conn *Conn) SendResult(format string, contents []byte) error {
    var err error
    var resultArgs = struct {
        Format string `json:"format"`
    }{
        Format: format,
    }
    resultArgsB, err := json.Marshal(resultArgs)
    if err != nil {
        return err
    }
    resultMsg := "result " + string(resultArgsB)
    err = websocket.Message.Send(conn.ws, resultMsg)
    if err != nil {
        return err
    }
    err = websocket.Message.Send(conn.ws, contents)
    if err != nil {
        return err
    }
    return nil
}

func (conn *Conn) Complete(e error) error {
    var err error
    var completeArgs = struct {
        Error string `json:"error,omitempty"`
    }{}
    switch e := e.(type) {
    case nil:
        // no-op
    case reply.Error:
        completeArgs.Error = e.Error()
    default:
        log.Print(e)
        completeArgs.Error = "Server error"
    }
    completeArgsB, err := json.Marshal(completeArgs)
    if err != nil {
        return err
    }
    completeMsg := "complete " + string(completeArgsB)
    err = websocket.Message.Send(conn.ws, completeMsg)
    if err != nil {
        return err
    }
    return nil
}

func (conn *Conn) HandleWith(task task) {
    conn.task = task
    go conn.receiveLoop()
}

func (conn *Conn) receiveLoop() {
    defer conn.Stop()

    const (
        addPrefix     = "add "
        optionsPrefix = "options "
        startPrefix   = "start "
        inputPrefix   = "input "
    )

    for {
        var message string
        err := websocket.Message.Receive(conn.ws, &message)
        if err != nil {
            if errors.Is(err, io.EOF) {
                return
            }
            select {
            case <-conn.Stopped:
            default:
                log.Println("websocket receive:", err)
            }
            return
        }
        switch {
        case strings.HasPrefix(message, addPrefix):
            var addArgs struct {
                Filename *string
                Hash     *string
                Restore  *bool
            }
            if err := json.Unmarshal(
                []byte(message[len(addPrefix):]), &addArgs,
            ); err != nil {
                conn.Deny(reply.Error("'add' arguments are not a correct JSON"))
                return
            }
            var contents []byte
            if addArgs.Filename == nil {
                conn.Deny(reply.Error("'add' must specify a 'filename'"))
                return
            }
            if addArgs.Restore != nil {
                if conn.cache == nil {
                    conn.Deny(reply.Error("'restore' not enabled"))
                    return
                }
                conn.Deny(reply.Error("XXX 'restore' not implemented"))
                return
            }
            if err := websocket.Message.Receive(conn.ws, &contents); err != nil {
                log.Print(err)
                return
            }
            // XXX check for total file size
            if err := conn.task.AddFile(*addArgs.Filename, contents); err != nil {
                conn.Deny(err)
                return
            }
        case strings.HasPrefix(message, optionsPrefix):
            var optionsArgs struct {
                Duration    *float64
                Format      *string
                StderrRedir *bool `json:"stderrRedir"`
                Verbosity   *int
            }
            if err := json.Unmarshal(
                []byte(message[len(optionsPrefix):]), &optionsArgs,
            ); err != nil {
                conn.Deny(reply.Error("'options' arguments are not a correct JSON"))
                return
            }
            if optionsArgs.Duration != nil {
                if err := conn.task.SetDuration(*optionsArgs.Duration); err != nil {
                    conn.Deny(err)
                    return
                }
            }
            if optionsArgs.Format != nil {
                if err := conn.task.SetFormat(*optionsArgs.Format); err != nil {
                    conn.Deny(err)
                    return
                }
            }
            if optionsArgs.StderrRedir != nil {
                if err := conn.task.SetStderrRedir(*optionsArgs.StderrRedir); err != nil {
                    conn.Deny(err)
                    return
                }
            }
            if optionsArgs.Verbosity != nil {
                if err := conn.task.SetVerbosity(*optionsArgs.Verbosity); err != nil {
                    conn.Deny(err)
                    return
                }
            }
        case strings.HasPrefix(message, startPrefix):
            var startArgs struct {
                Main *string
            }
            if err := json.Unmarshal(
                []byte(message[len(startPrefix):]), &startArgs,
            ); err != nil {
                conn.Deny(reply.Error("'start' arguments are not a correct JSON"))
                return
            }
            if startArgs.Main == nil {
                conn.Deny(reply.Error("'start' must specify a 'main' filename"))
                return
            }
            if err := conn.task.Start(*startArgs.Main); err != nil {
                conn.Deny(err)
                return
            }
        case strings.HasPrefix(message, inputPrefix):
            conn.Deny(reply.Error("XXX not implemented"))
            return
        default:
            conn.Deny(reply.Error("unknown command"))
            return
        }
    }
}

