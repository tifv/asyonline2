package main;

import (
    "bytes"
    "strings"
    "encoding/json"

    "io/ioutil"
    "path/filepath"
    "os"
    "os/exec"

    "net/http"
    "golang.org/x/net/websocket"

    "log"
)

func init() {
    if filepath.Separator != '/' {
        panic("file separator must be \"/\"")
    }
}

type runasyServer struct {
}

func main() {
    mux := http.NewServeMux()
    runasy := &runasyServer{}
    mux.Handle("/", websocket.Handler(func(conn *websocket.Conn) {
        runasy.runasy(conn)
    }))
    server := &http.Server{
        Addr: "localhost:8081",
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

func (server *runasyServer) runasy(conn *websocket.Conn) {
    defer conn.Close()
    defer log.Print("closing connection")

    //sources := make(map[string][]byte)
    var mainfile string
    var options = options{
        interactive: false,
        timeout:     0,
        format:      "svg",
        stderr:      "stdout",
        verbosity:   0,
    }

    const (
        addPrefix     = "add "
        optionsPrefix = "options "
        runCmd     = "run"
        inputCmd   = "input"
    )

    asyDir, err := ioutil.TempDir("/tmp", "tmp*")
    if err != nil {
        log.Print(err)
        return
    }
    defer os.RemoveAll(asyDir)

    // XXX replace most log.Print with actual error replies?

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
            }
            err = json.Unmarshal([]byte(message[len(addPrefix):]), &addArgs)
            if err != nil {
                log.Print("'add' arguments are not a correct JSON")
                return
            }
            if !strings.HasSuffix(addArgs.Filename, ".asy") {
                log.Print("'filename' must end with \".asy\"")
                return
            }
            if strings.Contains(addArgs.Filename, "/") {
                log.Print("'filename' cannot contain slash \"/\"")
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
            err = ioutil.WriteFile(
                filepath.Join(asyDir, addArgs.Filename),
                contents, 0o644)
            if err != nil {
                log.Print(err)
                return
            }
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
                log.Print("'options' arguments are not a correct JSON")
                return
            }
            if optionsArgs.Interactive != nil {
                if *optionsArgs.Interactive { // XXX
                    log.Print("XXX 'interactive' not implemented")
                    return
                }
                options.interactive = *optionsArgs.Interactive
            }
            if optionsArgs.Timeout != nil {
                if *optionsArgs.Timeout <= 0 {
                    log.Print("'timeout' must be positive")
                    return
                }
                options.timeout = *optionsArgs.Timeout
                // XXX do not ignore timeout
            }
            if optionsArgs.Format != nil {
                switch *optionsArgs.Format {
                case "svg", "pdf", "png":
                    options.format = *optionsArgs.Format
                default:
                    log.Print("'format' can only be \"svg\", \"pdf\", or \"png\"")
                    return
                }
            }
            if optionsArgs.Stderr != nil {
                switch *optionsArgs.Stderr {
                case "separate", "stdout":
                    if *optionsArgs.Stderr != "stdout" { // XXX
                        log.Print("XXX 'stderr' not implemented")
                        return
                    }
                    options.stderr = *optionsArgs.Stderr
                default:
                    log.Print("'stderr' can only be set to " +
                        "\"separate\" or \"stdout\"")
                    return
                }
            }
            if optionsArgs.Verbosity != nil {
                switch *optionsArgs.Verbosity {
                case 0, 1, 2, 3:
                    options.verbosity = *optionsArgs.Verbosity
                default:
                    log.Print("verbosity can only be set to 0, 1, 2, 3")
                    return
                }
            }
        case message == runCmd:
            if mainfile == "" {
                log.Print("no main file was set")
                return
            }
            break initLoop
        case message == inputCmd:
            log.Print("'input' cannot come before 'run'")
            return
        default:
            log.Print("unknown command " + message)
            return
        }
    }

    // XXX some commands may still come after 'run'

    outname := filepath.Join(asyDir, "output." + options.format)

    asyArgs := []string{
        "-offscreen",
        "-outformat", options.format,
        mainfile,
        "-outname", outname,
    }
    switch options.verbosity {
    case 0:
    case 1:
        asyArgs = append(asyArgs, "-v")
    case 2:
        asyArgs = append(asyArgs, "-vv")
    case 3:
        asyArgs = append(asyArgs, "-vvv")
    }

    // XXX separate output and input dir ?
    cmd := exec.Command("asy", asyArgs...)
    cmd.Dir = asyDir
    var asyStdout bytes.Buffer
    cmd.Stdout = &asyStdout
    cmd.Stderr = &asyStdout
    if err := cmd.Start(); err != nil {
        log.Print(err)
        return
    }
    var asyErr error = cmd.Wait()
    var asyErrMsg string
    if asyErr != nil {
        asyErrMsg = asyErr.Error()
    }

    { // send output
        var err error
        // XXX proper JSON encoding?
        err = websocket.Message.Send(conn, `output {"stream": "stdout"}`)
        if err != nil {
            log.Print(err)
            return
        }
        err = websocket.Message.Send(conn, asyStdout.Bytes())
        if err != nil {
            log.Print(err)
            return
        }
    }

    { // send result
        var result []byte
        var err error
        result, err = ioutil.ReadFile(outname)
        if err != nil {
            if asyErrMsg == "" {
                asyErrMsg = "No image output"
            }
            goto noresult
        }
        // XXX proper JSON encoding?
        err = websocket.Message.Send(conn, `result {"format": "`+options.format+`"}`)
        if err != nil {
            log.Print(err)
            return
        }
        err = websocket.Message.Send(conn, result)
        if err != nil {
            log.Print(err)
            return
        }
    }
    noresult:

    { // send complete
        var err error
        completeArgsB, err := json.Marshal(struct {
            Success bool   `json:"success"`
            Error   string `json:"error,omitempty"`
            Time    int    `json:"success,omitempty"`
        }{
            Success: asyErr == nil,
            Error:   asyErrMsg,
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
    }

}

