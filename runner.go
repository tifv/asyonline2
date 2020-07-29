package main

import (
    "bytes"
    "strings"
    "path/filepath"
    "errors"

    "io/ioutil"
    "os"
    "os/exec"

    "log"
)

// implements handler
type runner struct {
    gate chan bool
}

func (rr *runner) init() {
    rr.gate = make(chan bool, 1)
    rr.gate <- true
}

// sequence:
// create directory
// write some files
// execute asy
// read results
// send results
// remove directory

func (rr *runner) newTask(c *conn) (task, *errreply) {
    // XXX impose some kind of resource limit
    <-rr.gate
    r := &run{conn: c}
    halted, workdir, reply := r.init()
    if reply != nil {
        rr.gate <- true
        return nil, reply
    }
    go r.watchloop(halted, workdir, func() {
        rr.gate <- true
    })
    return r, nil
}

type run struct {
    *conn
    halted    chan<- bool
    workdir   string
    timeout   int
    format    string
    stderr    string
    verbosity int
}

func (r *run) init() (halted chan bool, workdir string, reply *errreply) {
    halted = make(chan bool, 2)
    r.halted = halted
    var err error
    workdir, err = ioutil.TempDir("/tmp", "tmp*")
    if err != nil {
        log.Print(err)
        reply = &errreply{"Server error"}
        return
    }
    return
}

func (r *run) watchloop(halted chan bool, workdir string, finish func()) {
    defer finish()
    <-halted
    if err := os.RemoveAll(workdir); err != nil {
        log.Print(err)
    }
    r.conn.halt()
}

func (r *run) halt() {
    if r.halted != nil {
        r.halted <- true;
    }
}

func (r *run) addFile(filename string, contents []byte) *errreply {
    if reply := checkFilename(filename); reply != nil {
        return reply
    }
    var err error
    err = ioutil.WriteFile(
        filepath.Join(r.workdir, filename),
        contents, 0o644)
    if err != nil {
        log.Print(err)
        return &errreply{"Server error"}
    }
    return nil
}

func (r *run) setTimeout(timeout int) *errreply {
    // XXX timeout
    return &errreply{"XXX timeout not implemented"}
}

func (r *run) setFormat(format string) *errreply {
    switch format {
    case "svg", "pdf", "png":
    default:
        return &errreply{"'format' can only be \"svg\", \"pdf\", or \"png\""}
    }
        r.format = format
    return nil
}

func (r *run) setStderr(stderr string) *errreply {
    switch stderr {
    case "separate", "stdout":
    default:
        return &errreply{"'stderr' can only be set to \"separate\" or \"stdout\""}
    }
    // XXX stderr
    return &errreply{"XXX 'stderr' not implemented"}
}

func (r *run) setVerbosity(verbosity int) *errreply {
    switch verbosity {
    case 0, 1, 2, 3:
    default:
        return &errreply{"'verbosity' can only be set to 0, 1, 2, 3"}
    }
    r.verbosity = verbosity
    return nil
}

func (r *run) run(mainname string) *errreply {
    if reply := checkFilename(mainname); reply != nil {
        return reply
    }
    go r.runloop(mainname)
    return nil
}

func (r *run) runloop(mainname string) {
    outname := filepath.Join(r.workdir, "output." + r.format)
    asyArgs := []string{
        "-offscreen",
        "-outformat", r.format,
        mainname,
        "-outname", outname,
    }
    switch r.verbosity {
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
    cmd.Dir = r.workdir
    var asyStdout bytes.Buffer
    cmd.Stdout = &asyStdout
    cmd.Stderr = &asyStdout
    if err := cmd.Start(); err != nil {
        log.Print(err)
        err = r.conn.sendComplete(false, &errreply{"Server error"})
        if err != nil {
            log.Print(err)
        }
        r.halt()
        return
    }
    var asyErr error = cmd.Wait()

    if err := r.conn.sendOutput("stdout", asyStdout.Bytes()); err != nil {
        log.Print(err)
        r.halt()
        return
    }

    { // send result
        var result []byte
        var err error
        result, err = ioutil.ReadFile(outname)
        if err != nil {
            if asyErr == nil {
                asyErr = errors.New("No image")
            }
            goto noresult
        }
        if err := r.conn.sendResult(r.format, result); err != nil {
            log.Print(err)
            r.halt()
            return
        }
    }
    noresult:

    if asyErr == nil {
        if err := r.conn.sendComplete(true, nil); err != nil {
            log.Print(err)
            r.halt()
            return
        }
    } else {
        if err := r.conn.sendComplete(false, &errreply{"Execution failed"}); err != nil {
            log.Print(err)
            r.halt()
            return
        }
    }
}

func checkFilename(filename string) *errreply {
    if !strings.HasSuffix(filename, ".asy") {
        return &errreply{"filename must end with \".asy\""}
    }
    if strings.ContainsRune(filename, '/') {
        return &errreply{"filename cannot contain slash \"/\""}
    }
    const sep = filepath.Separator
    if '/' != sep && strings.ContainsRune(filename, sep) {
        return &errreply{"'filename' cannot contain separator \"" +
            string(sep) + "\""}
    }
    return nil
}

