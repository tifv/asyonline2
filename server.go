package main

import (
    "strings"
    "encoding/json"

    "net/http"
    "golang.org/x/net/websocket"

    "errors"
    "log"
)

type errreply struct {
    string
}

type handler interface {
    newTask(c *conn) (task, *errreply)
}

type task interface {
    addFile(filename string, contents []byte) *errreply
    setTimeout(timeout int) *errreply
    setFormat(format string) *errreply
    setStderr(stderr string) *errreply
    setVerbosity(verbosity int) *errreply
    run(mainname string) *errreply
    halt()
}

type server struct {
    handler
    *cache
}

func (s server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    websocket.Server{
        Config: websocket.Config{Protocol: []string{"asyonline/asy"}},
        Handshake: func(config *websocket.Config, req *http.Request) error {
            // XXX check origin?
            for _, protocol := range config.Protocol {
                switch protocol {
                case "asyonline/asy":
                    config.Protocol = []string{protocol}
                    return nil
                }
            }
            return errors.New("unknown websocket sub-protocols")
        },
        Handler: websocket.Handler(func(wsconn *websocket.Conn) {
            defer wsconn.Close()
            var c = s.newConn(wsconn)
            var reply *errreply
            c.task, reply = s.handler.newTask(c)
            if reply != nil {
                c.deny(*reply)
                return
            }
            defer c.task.halt()
            c.loop()
        }),
    }.ServeHTTP(w, req)
}

func (s server) newConn(wsconn *websocket.Conn) *conn {
    return &conn{ws: wsconn, cache: s.cache}
}

type conn struct {
    task
    ws *websocket.Conn
    halted chan<- bool
    *cache
}

func (c *conn) loop() {
    halted := make(chan bool, 2)
    c.halted = halted
    go c.readloop()
    <-halted
}

func (c *conn) deny(reply errreply) {
    var err error
    denyArgsB, err := json.Marshal(struct {
        Error string `json:"error"`
    }{
        Error: reply.string,
    })
    if err != nil {
        log.Print(err)
        return
    }
    denyMsg := "deny " + string(denyArgsB)
    err = websocket.Message.Send(c.ws, denyMsg)
    if err != nil {
        log.Print(err)
        return
    }
}

func (c *conn) sendOutput(stream string, output []byte) error {
    var err error
    var outputArgs = struct {
        Stream   string `json:"stream"`
    }{
        Stream: stream,
    }
    outputArgsB, err := json.Marshal(outputArgs)
    if err != nil {
        return err
    }
    outputMsg := "output " + string(outputArgsB)
    err = websocket.Message.Send(c.ws, outputMsg)
    if err != nil {
        return err
    }
    err = websocket.Message.Send(c.ws, output)
    if err != nil {
        return err
    }
    return nil
}

func (c *conn) sendResult(format string, contents []byte) error {
    var err error
    var resultArgs = struct {
        Format string   `json:"format"`
    }{
        Format: format,
    }
    resultArgsB, err := json.Marshal(resultArgs)
    if err != nil {
        return err
    }
    resultMsg := "result " + string(resultArgsB)
    err = websocket.Message.Send(c.ws, resultMsg)
    if err != nil {
        return err
    }
    err = websocket.Message.Send(c.ws, contents)
    if err != nil {
        return err
    }
    return nil
}

func (c *conn) sendComplete(success bool, reply *errreply) error {
    var err error
    var completeArgs = struct {
        Success bool   `json:"success"`
        Error   string `json:"error,omitempty"`
    }{
        Success: success,
    }
    if reply != nil {
        completeArgs.Error = (*reply).string
    }
    completeArgsB, err := json.Marshal(completeArgs)
    if err != nil {
        return err
    }
    completeMsg := "complete " + string(completeArgsB)
    err = websocket.Message.Send(c.ws, completeMsg)
    if err != nil {
        return err
    }
    return nil
}

func (c *conn) halt() {
    if c.halted != nil {
        c.halted <- true;
    }
}

func (c *conn) readloop() {
    defer c.halt()

    const (
        addPrefix     = "add "
        optionsPrefix = "options "
        runPrefix     = "run "
        inputPrefix   = "input "
    )

    for {
        var message string
        err := websocket.Message.Receive(c.ws, &message)
        if err != nil {
            if err.Error() == "EOF" {
                return
            }
            log.Println("receive error:", err)
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
                c.deny(errreply{"'add' arguments are not a correct JSON"})
                return
            }
            var contents []byte
            if addArgs.Filename == nil {
                c.deny(errreply{"'add' must specify a 'filename'"})
                return
            }
            if addArgs.Restore != nil {
                if c.cache == nil {
                    c.deny(errreply{"'restore' not enabled"})
                    return
                }
                c.deny(errreply{"XXX 'restore' not implemented"})
                return
            }
            if err := websocket.Message.Receive(c.ws, &contents); err != nil {
                log.Print(err)
                return
            }
            // XXX check for total file size
            if reply := c.task.addFile(*addArgs.Filename, contents); reply != nil {
                c.deny(*reply)
                return
            }
        case strings.HasPrefix(message, optionsPrefix):
            var optionsArgs struct {
                Timeout     *int
                Format      *string
                Stderr      *string
                Verbosity   *int
            }
            if err = json.Unmarshal(
                []byte(message[len(optionsPrefix):]), &optionsArgs,
            ); err != nil {
                c.deny(errreply{"'options' arguments are not a correct JSON"})
                return
            }
            if optionsArgs.Timeout != nil {
                if *optionsArgs.Timeout < 0 {
                    c.deny(errreply{"'timeout' must be nonnegative"})
                    return
                }
                if reply := c.task.setTimeout(*optionsArgs.Timeout); reply != nil {
                    c.deny(*reply)
                    return
                }
            }
            if optionsArgs.Format != nil {
                if reply := c.task.setFormat(*optionsArgs.Format); reply != nil {
                    c.deny(*reply)
                    return
                }
            }
            if optionsArgs.Stderr != nil {
                switch *optionsArgs.Stderr {
                default:
                    c.deny(errreply{"'stderr' can only be set to \"separate\" or \"stdout\""})
                    return
                case "separate", "stdout":
                }
                if reply := c.task.setStderr(*optionsArgs.Stderr); reply != nil {
                    c.deny(*reply)
                    return
                }
            }
            if optionsArgs.Verbosity != nil {
                switch *optionsArgs.Verbosity {
                default:
                    c.deny(errreply{"'verbosity' can only be set to 0, 1, 2, 3"})
                    return
                case 0, 1, 2, 3:
                }
                if reply := c.task.setVerbosity(*optionsArgs.Verbosity); reply != nil {
                    c.deny(*reply)
                    return
                }
            }
        case strings.HasPrefix(message, runPrefix):
            var runArgs struct {
                Main *string
            }
            if err = json.Unmarshal(
                []byte(message[len(runPrefix):]), &runArgs,
            ); err != nil {
                c.deny(errreply{"'run' arguments are not a correct JSON"})
                return
            }
            if runArgs.Main == nil {
                c.deny(errreply{"'run' must specify a 'main' filename"})
                return
            }
            if reply := c.task.run(*runArgs.Main); reply != nil {
                c.deny(*reply)
                return
            }
        case strings.HasPrefix(message, inputPrefix):
            c.deny(errreply{"XXX not implemented"})
            return
        default:
            c.deny(errreply{"unknown command"})
            return
        }
    }
}

type cache struct {
}

