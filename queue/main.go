package main;

import (
    "log"
    "strings"
    "bytes"
    "strconv"
    "os"
    "os/exec"
    "io/ioutil"
    "path/filepath"
    "net/http"
    "golang.org/x/net/websocket"
)

func main() {
    mux := http.NewServeMux()
    mux.Handle("/asy", websocket.Handler(asy0))
    server := &http.Server{
        Addr: "localhost:8080",
        Handler: mux,
    }
    log.Fatal(server.ListenAndServe())
}

func asy0(conn *websocket.Conn) {
    defer conn.Close();

    sources := make(map [string] []byte)
    var format string
    var timeout uint = 0
    var main_filename string

    for {
        var message string
        if err := websocket.Message.Receive(conn, &message); err != nil {
            log.Print(err); return
            // TODO respond with error
        }
        const add_msg = "add"
        const format_msg = "format"
        const timeout_msg = "timeout"
        const run_msg = "run"
        if strings.HasPrefix(message, add_msg + " ") {
            filename := message[len(add_msg + " "):]
            // XXX check extension
            var contents []byte
            if err := websocket.Message.Receive(conn, &contents); err != nil {
                log.Print(err); return
                // TODO respond with error
            }
            // XXX check for total file size
            if strings.Contains(filename, "/") {
                log.Print("invalid filename " + format)
                return
                // TODO respond with error
            }
            sources[filename] = contents
        } else if strings.HasPrefix(message, format_msg + " ") {
            format = message[len(format_msg + " "):]
            if format != "svg" {
                log.Print("invalid format " + format)
                return
            }
        } else if strings.HasPrefix(message, timeout_msg + " ") {
            new_timeout, err := strconv.ParseUint(
                message[len(timeout_msg + " "):],
                10, 64 )
            if err != nil {
                log.Print(err); return
                // XXX respond with error
            }
            timeout = uint(new_timeout)
        } else if strings.HasPrefix(message, run_msg + " ") {
            main_filename = message[len(run_msg + " "):]
            // XXX check extension
            if strings.Contains(main_filename, "/") {
                log.Print("invalid main filename " + main_filename)
                return
                // TODO respond with error
            }
            if _, ok := sources[main_filename]; !ok {
                log.Print("invalid main filename " + main_filename)
                return
            }
            break;
        } else {
            return
        }
    }

    dir, err := ioutil.TempDir("/home/july/run", "tmp*")
    if err != nil {
        log.Print(err); return
    }

    defer os.RemoveAll(dir) // clean up

    for filename, contents := range sources {
        err := ioutil.WriteFile(filepath.Join(dir, filename), contents, 0o644)
        if err != nil {
            log.Print(err); return
            // TODO respond with error
        }
    }

    outname := filepath.Join(dir, "output." + format)

    // separate output and input dir
    cmd := exec.Command( "asy",
        "-offscreen",
        "-outformat", format,
        main_filename,
        "-outname", outname,
        "-vvv",
    )
    cmd.Dir = dir
    var asy_stdout bytes.Buffer
    cmd.Stdout = &asy_stdout
    cmd.Stderr = &asy_stdout
    if err := cmd.Start(); err != nil {
        log.Print(err); return
    }
    asy_err := cmd.Wait()

    _ = timeout // XXX

    if err := websocket.Message.Send(conn, "stdout"); err != nil {
        log.Print(err); return
    }

    if err := websocket.Message.Send(conn, asy_stdout.Bytes()); err != nil {
        log.Print(err); return
    }

    var asy_output []byte; {
        var err error
        asy_output, err = ioutil.ReadFile(outname)
        if err != nil {
            if err := websocket.Message.Send(conn, "error No image output"); err != nil {
                log.Print(err); conn.Close(); return
            }
            return
        }

        if err := websocket.Message.Send(conn, format); err != nil {
            log.Print(err); return
        }

        if err := websocket.Message.Send(conn, asy_output); err != nil {
            log.Print(err); return
        }

    }

    if asy_err != nil {
        log.Print(asy_err)
        if err := websocket.Message.Send(conn, "error occured"); err != nil {
            log.Print(err); return
        }
    } else {
        if err := websocket.Message.Send(conn, "success"); err != nil {
            log.Print(err); return
        }
    }
    return
}

