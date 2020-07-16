package main

import (
    "strings"
    "encoding/json"

    "net/http"

    "golang.org/x/net/websocket"

    "log"
)

type queueServer struct {
    //runasyClients chan *runasyClient
    *runasyClient
}

type runasyClient struct {
    //outcoming chan interface{}
    addr string
}

func main() {
    mux := http.NewServeMux()
    queue := &queueServer{}
    queue.runasyClient = &runasyClient{addr: "localhost:8081"}
    mux.Handle("/asy", websocket.Handler(func(conn *websocket.Conn) {
        queue.queue(conn)
    }))
    server := &http.Server{
        Addr:    "localhost:8080",
        Handler: mux,
    }
    err := server.ListenAndServe()
    log.Fatal(err)
}

type options struct {
    interactive bool
    timeout     int
    format      string
    stderr      string
    verbosity   int
}

type resultMessage struct {
    format   string
    contents []byte
}

type outputMessage struct {
    stream string
    output []byte
}

type completeMessage struct {
    success bool
    error   string
    time    int
}

func (server *queueServer) queue(conn *websocket.Conn) {
    defer conn.Close()
    defer log.Print("closing connection")

    sources := make(map[string][]byte)
    var mainfile string
    var options = options{
        interactive: false,
        timeout:     0,
        format:      "svg",
        stderr:      "stdout",
        verbosity:   3,
    }

    const (
        addPrefix     = "add "
        optionsPrefix = "options "
        runCmd     = "run"
        inputCmd   = "input"
    )

    deny := func(message string) {
        var err error
        deniedArgsB, err := json.Marshal(struct {
            Error string `json:"error"`
        }{
            Error: message,
        })
        if err != nil {
            log.Print(err)
            return
        }
        deniedMsg := "denied " + string(deniedArgsB)
        err = websocket.Message.Send(conn, deniedMsg)
        if err != nil {
            log.Print(err)
            return
        }
    }

    initLoop:
    for {
        var message string
        err := websocket.Message.Receive(conn, &message)
        if err != nil {
            log.Print(err)
            return
        }
        log.Printf("message received: %s", message)
        switch {
        case strings.HasPrefix(message, addPrefix):
            var err error
            var addArgs struct {
                Filename string
                Main     bool
                Hash     string
                Restore  bool
            }
            err = json.Unmarshal([]byte(message[len(addPrefix):]), &addArgs)
            if err != nil {
                deny("'add' arguments are not a correct JSON")
                return
            }
            if !strings.HasSuffix(addArgs.Filename, ".asy") {
                deny("'filename' must end with \".asy\"")
                return
            }
            if strings.Contains(addArgs.Filename, "/") {
                deny("'filename' cannot contain slash \"/\"")
                return
            }
            if addArgs.Restore {
                deny("XXX 'restore' not implemented")
                return
            }
            if addArgs.Main {
                mainfile = addArgs.Filename
            }
            var contents []byte
            err = websocket.Message.Receive(conn, &contents)
            if err != nil {
                log.Print(err)
                return
            }
            // XXX check for total file size
            sources[addArgs.Filename] = contents
        case strings.HasPrefix(message, optionsPrefix):
            var err error
            var optionsArgs struct {
                Interactive *bool
                Timeout     *int
                Format      *string
                Stderr      *string
                Verbosity   *int
            }
            err = json.Unmarshal([]byte(message[len(optionsPrefix):]), &optionsArgs)
            if err != nil {
                deny("'options' arguments are not a correct JSON")
                return
            }
            if optionsArgs.Interactive != nil {
                if *optionsArgs.Interactive { // XXX
                    deny("XXX 'interactive' not implemented")
                    return
                }
                options.interactive = *optionsArgs.Interactive
            }
            if optionsArgs.Timeout != nil {
                if *optionsArgs.Timeout < 0 {
                    deny("'timeout' must be nonnegative")
                    return
                }
                if *optionsArgs.Timeout != 0 { // XXX
                    deny("XXX 'timeout' not implemented")
                    return
                }
                options.timeout = *optionsArgs.Timeout
            }
            if optionsArgs.Format != nil {
                switch *optionsArgs.Format {
                case "svg", "pdf", "png":
                    options.format = *optionsArgs.Format
                default:
                    deny("'format' can only be \"svg\", \"pdf\", or \"png\"")
                    return
                }
            }
            if optionsArgs.Stderr != nil {
                switch *optionsArgs.Stderr {
                case "separate", "stdout":
                    if *optionsArgs.Stderr != "stdout" { // XXX
                        deny("XXX 'stderr' not implemented")
                        return
                    }
                    options.stderr = *optionsArgs.Stderr
                default:
                    deny("'stderr' can only be set to " +
                        "\"separate\" or \"stdout\"")
                    return
                }
            }
            if optionsArgs.Verbosity != nil {
                switch *optionsArgs.Verbosity {
                case 0, 1, 2, 3:
                    options.verbosity = *optionsArgs.Verbosity
                default:
                    deny("verbosity can only be set to 0, 1, 2, 3")
                    return
                }
            }
        case message == runCmd:
            if mainfile == "" {
                deny("no main file was set")
                return
            }
            break initLoop
        case message == inputCmd:
            deny("'input' cannot come before 'run'")
            return
        default:
            deny("unknown command " + message)
            return
        }
    }

    //runasy := <-server.runasyClients
    runasy := server.runasyClient
    outcoming := runasy.run(sources, mainfile, options)

    var completeSent bool = false
    for message := range outcoming {
        switch message := message.(type) {
        case resultMessage:
            var err error
            resultArgsB, err := json.Marshal(struct {
                Format string `json:"format"`
            }{
                Format: message.format,
            })
            if err != nil {
                log.Print(err)
                return
            }
            resultMsg := "result " + string(resultArgsB)
            err = websocket.Message.Send(conn, resultMsg)
            if err != nil {
                log.Print(err)
                return
            }
            err = websocket.Message.Send(conn, message.contents)
            if err != nil {
                log.Print(err)
                return
            }
        case outputMessage:
            var err error
            outputArgsB, err := json.Marshal(struct {
                Stream string `json:"stream"`
            }{
                Stream: message.stream,
            })
            if err != nil {
                log.Print(err)
                return
            }
            outputMsg := "output " + string(outputArgsB)
            err = websocket.Message.Send(conn, outputMsg)
            if err != nil {
                log.Print(err)
                return
            }
            err = websocket.Message.Send(conn, message.output)
            if err != nil {
                log.Print(err)
                return
            }
        case completeMessage:
            var err error
            completeArgsB, err := json.Marshal(struct {
                Success bool   `json:"success"`
                Error   string `json:"error,omitempty"`
                Time    int    `json:"success,omitempty"`
            }{
                Success: message.success,
                Error:   message.error,
                Time:    message.time,
            })
            if err != nil {
                log.Print(err)
                return
            }
            completeMsg := "complete " + string(completeArgsB)
            err = websocket.Message.Send(conn, completeMsg)
            if err != nil {
                log.Print(err)
                return
            }
            completeSent = true
        default:
            log.Printf("%#v", message)
            return
        }
    }
    if !completeSent {
        completeMsg := "complete {\"success\": false, \"error\": \"server error\"}"
        err := websocket.Message.Send(conn, completeMsg)
        if err != nil {
            log.Print(err)
            return
        }
    }
}

func (runasy *runasyClient) run(
    sources map[string][]byte, mainfile string, options options,
) <-chan interface{} {
    outcoming := make(chan interface{})
    conn, err := websocket.Dial("ws://"+runasy.addr+"/", "", "http://localhost/")
    if err != nil {
        log.Print(err)
        close(outcoming)
        return outcoming
    }
    go runasy.runloop(conn, outcoming, sources, mainfile, options)
    return outcoming
}

func (runasy *runasyClient) runloop(
    conn *websocket.Conn, outcoming chan<- interface{},
    sources map[string][]byte, mainfile string, options options,
) {
    defer close(outcoming)
    defer conn.Close()

    const (
        resultPrefix   = "result "
        outputPrefix   = "output "
        completePrefix = "complete "
    )

    // XXX while we are sending task details, shouldn't we already be
    // listening for errors?
    // Or, rather, listening for errors should be the loop, and sending details
    // should be out of loop

    for filename, contents := range sources {
        var err error
        addArgsB, err := json.Marshal(struct {
            Filename string `json:"filename"`
            Main     bool   `json:"main"`
        }{
            Filename: filename,
            Main:     filename == mainfile,
        })
        if err != nil {
            log.Print(err)
            return
        }
        addMsg := "add " + string(addArgsB)
        err = websocket.Message.Send(conn, addMsg)
        if err != nil {
            log.Print(err)
            return
        }
        err = websocket.Message.Send(conn, contents)
        if err != nil {
            log.Print(err)
            return
        }
    }

    { // options
        var err error
        if options.timeout == 0 { // XXX
            options.timeout = 10000
        }
        optionsArgsB, err := json.Marshal(struct {
            Interactive bool   `json:"interactive"`
            Timeout     int    `json:"timeout"`
            Format      string `json:"format"`
            Stderr      string `json:"stderr"`
            Verbosity   int    `json:"verbosity"`
        }{
            Interactive: options.interactive,
            Timeout:     options.timeout,
            Format:      options.format,
            Stderr:      options.stderr,
            Verbosity:   options.verbosity,
        })
        if err != nil {
            log.Print(err)
            return
        }
        optionsMsg := "options " + string(optionsArgsB)
        err = websocket.Message.Send(conn, optionsMsg)
        if err != nil {
            log.Print(err)
            return
        }
    }

    { // run
        var err error
        err = websocket.Message.Send(conn, "run")
        if err != nil {
            log.Print(err)
            return
        }
    }

    for {
        var message string
        err := websocket.Message.Receive(conn, &message)
        if err != nil {
            log.Print(err)
            return
        }
        switch {
        case strings.HasPrefix(message, resultPrefix):
            var err error
            var resultArgs struct {
                Format string
            }
            err = json.Unmarshal([]byte(message[len(resultPrefix):]), &resultArgs)
            if err != nil {
                log.Print("'result' arguments are not a correct JSON")
                return
            }
            var contents []byte
            err = websocket.Message.Receive(conn, &contents)
            if err != nil {
                log.Print(err)
                return
            }
            outcoming <- resultMessage{
                format:   resultArgs.Format,
                contents: contents,
            }
        case strings.HasPrefix(message, outputPrefix):
            var err error
            var outputArgs struct {
                Stream string
            }
            err = json.Unmarshal([]byte(message[len(outputPrefix):]), &outputArgs)
            if err != nil {
                log.Print("'output' arguments are not a correct JSON")
                return
            }
            if outputArgs.Stream == "" {
                log.Print("'stream' must be defined")
                return
            }
            var contents []byte
            err = websocket.Message.Receive(conn, &contents)
            if err != nil {
                log.Print(err)
                return
            }
            outcoming <- outputMessage{
                stream: outputArgs.Stream,
                output: contents,
            }
        case strings.HasPrefix(message, completePrefix):
            var err error
            var completeArgs struct {
                Success bool
                Error   string
                Time    int
            }
            err = json.Unmarshal([]byte(message[len(completePrefix):]), &completeArgs)
            if err != nil {
                log.Print("'complete' arguments are not a correct JSON")
                return
            }
            outcoming <- completeMessage{
                success: completeArgs.Success,
                error:   completeArgs.Error,
                time:    completeArgs.Time,
            }
        default:
            log.Print("unknown command", message)
            return
        }
    }
}

// vim: set fdm=marker :
