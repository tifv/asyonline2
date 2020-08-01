package asy

import (
    "errors"
    "strings"
    "sync"
    "time"

    "fmt"
    "io"
    "path/filepath"

    "io/ioutil"
    "log"
    "os"

    "golang.org/x/sys/unix"

    "../server/reply"
)

type void = struct{}

type conn interface {
    Stop()
    //Deny(err error)
    SendOutput(stream string, output []byte) error
    SendResult(format string, contents []byte) error
    Complete(err error) error
}

type Task struct {
    stopper
    conn        conn
    workdir     string
    timer       timer
    format      string
    stderrRedir bool
    verbosity   int
}

type stopper struct {
    Stopped  <-chan void
    stopFunc func()
}

func newStopper() stopper {
    var stopped = make(chan void)
    var once sync.Once
    var stop = func() { close(stopped) }
    return stopper{
        Stopped:  stopped,
        stopFunc: func() { once.Do(stop) },
    }
}

func (s stopper) Stop() {
    s.stopFunc()
}

func NewTask(conn conn) (*Task, error) {
    task := &Task{
        conn:        conn,
        stopper:     newStopper(),
        format:      "svg",
        stderrRedir: true,
        verbosity:   0,
    }
    task.timer = newTimer(task.Stopped)
    var err error
    task.workdir, err = tempDir(task.Stopped)
    if err != nil {
        log.Print(err)
        return nil, err
    }
    return task, nil
}

func tempDir(stopped <-chan void) (string, error) {
    var workdir string
    workdir, err := ioutil.TempDir("/tmp", "tmp*")
    if err != nil {
        log.Print(err)
        return workdir, err
    }
    go func(workdir string, stopped <-chan void) {
        <-stopped
        if err := os.RemoveAll(workdir); err != nil {
            log.Print(err)
        }
    }(workdir, stopped)
    return workdir, nil
}

type timer struct {
    durations chan<- time.Duration
    start     chan<- void
    end       <-chan void
}

func newTimer(stopped <-chan void) timer {
    durations := make(chan time.Duration)
    start := make(chan void)
    end := make(chan void)
    go timerLoop(durations, start, end, stopped)
    return timer{durations, start, end}
}

func timerLoop(
    durations <-chan time.Duration, start <-chan void, end chan<- void,
    stopped <-chan void,
) {
    defer close(end)
    var duration time.Duration = -1
    var st, et time.Time
    var endish <-chan void
    var makeEndish = func() <-chan void {
        endish := make(chan void)
        go func(et time.Time, endish chan<- void) {
            time.Sleep(time.Until(et))
            close(endish)
        }(et, endish)
        return endish
    }
    for {
        select {
        case d := <-durations:
            if d < 0 {
                panic("timer duration must not be negative")
            }
            if d == 0 {
                return
            }
            if duration < 0 || d < duration {
                duration = d
                if !st.IsZero() {
                    et = st.Add(duration)
                    endish = makeEndish()
                }
            }
        case <-start:
            start = nil
            st = time.Now()
            if duration >= 0 {
                et = st.Add(duration)
                endish = makeEndish()
            }
        case <-stopped:
            return
        case <-endish:
            return
        }
    }
}

func (task *Task) AddFile(filename string, contents []byte) error {
    if err := checkFilename(filename); err != nil {
        return err
    }
    if err := ioutil.WriteFile(
        filepath.Join(task.workdir, filename),
        contents, 0o644,
    ); err != nil {
        log.Print(err)
        return err
    }
    return nil
}

func checkFilename(filename string) error {
    if !strings.HasSuffix(filename, ".asy") {
        return reply.Error("filename must end with \".asy\"")
    }
    if strings.ContainsRune(filename, '/') {
        return reply.Error("filename cannot contain slash \"/\"")
    }
    const sep = filepath.Separator
    if '/' != sep && strings.ContainsRune(filename, sep) {
        return reply.Error("'filename' cannot contain separator \"" +
            string(sep) + "\"")
    }
    return nil
}

func (task *Task) SetDuration(duration float32) error {
    if duration < 0 {
        return reply.Error("'duration' must be nonnegative")
    }
    task.timer.durations <- time.Duration(int64(1e9 * duration))
    return nil
}

func (task *Task) SetFormat(format string) error {
    switch format {
    case "svg", "pdf", "png":
    default:
        return reply.Error(
            "'format' can only be \"svg\", \"pdf\", or \"png\"")
    }
    task.format = format
    return nil
}

func (task *Task) SetStderrRedir(stderrRedir bool) error {
    task.stderrRedir = stderrRedir
    return nil
}

func (task *Task) SetVerbosity(verbosity int) error {
    switch verbosity {
    case 0, 1, 2, 3:
    default:
        return reply.Error(
            "'verbosity' can only be set to 0, 1, 2, 3")
    }
    task.verbosity = verbosity
    return nil
}

func (task *Task) Run(mainname string) error {
    if err := checkFilename(mainname); err != nil {
        return err
    }
    task.timer.durations <- time.Duration(30e9)
    // XXX avoid race conditions in setting format, stderr, etc.
    // (probably just disable setting them after starting runloop)
    go task.runloop(mainname)
    return nil
}

func (task *Task) runloop(mainname string) {
    defer task.Stop()
    outname := filepath.Join(task.workdir, "output."+task.format)
    asyArgs := []string{
        "asy",
        "-offscreen",
        "-outformat", task.format,
        mainname,
        "-outname", outname,
    }
    switch task.verbosity {
    case 0:
    case 1:
        asyArgs = append(asyArgs, "-v")
    case 2:
        asyArgs = append(asyArgs, "-vv")
    case 3:
        asyArgs = append(asyArgs, "-vvv")
    default:
        return
    }

    var asyProc *os.Process
    var asyProcStarted = make(chan void)
    sigpipe := func() error {
        select {
        case <-asyProcStarted:
        default:
            return errors.New("Process has not started yet")
        }
        return asyProc.Signal(unix.SIGPIPE)
    }
    var asyProcAttr = os.ProcAttr{
        Dir:   task.workdir,
        Files: make([]*os.File, 0, 3),
    }

    var loose_files = make([]*os.File, 0, 2)
    var close_loose_files = func() {
        for len(loose_files) > 0 {
            var file *os.File
            file, loose_files = loose_files[0], loose_files[1:]
            if file != nil {
                file.Close()
            }
        }
    }
    defer close_loose_files()
    {
        var stdin *os.File
        stdin, err := os.Open(os.DevNull)
        if err != nil {
            return
        }
        loose_files = append(loose_files, stdin)
        asyProcAttr.Files = append(asyProcAttr.Files, stdin)
    }
    var stdoutDone <-chan error
    var stderrDone <-chan error
    {
        var stdout *os.File
        var err error
        stdout, stdoutDone, err = runReader(
            func(output []byte) error {
                return task.conn.SendOutput("stdout", output)
            }, sigpipe)
        if err != nil {
            if errx := task.conn.Complete(err); errx != nil {
                log.Print(errx)
            }
            return
        }
        loose_files = append(loose_files, stdout)
        asyProcAttr.Files = append(asyProcAttr.Files, stdout)
    }
    if task.stderrRedir {
        asyProcAttr.Files = append(asyProcAttr.Files, asyProcAttr.Files[1])
        stderrDone = stdoutDone
    } else {
        var stderr *os.File
        var err error
        stderr, stderrDone, err = runReader(
            func(output []byte) error {
                return task.conn.SendOutput("stderr", output)
            }, sigpipe)
        if err != nil {
            if errx := task.conn.Complete(err); errx != nil {
                log.Print(errx)
            }
            return
        }
        loose_files = append(loose_files, stderr)
        asyProcAttr.Files = append(asyProcAttr.Files, stderr)
    }

    {
        var err error
        asyProc, err = os.StartProcess("/usr/bin/asy", asyArgs, &asyProcAttr)
        if err != nil {
            errx := task.conn.Complete(err)
            if errx != nil {
                log.Print(errx)
            }
            return
        }
    }
    close(asyProcStarted)
    close(task.timer.start)
    close_loose_files()
    var (
        dead   chan<- void
        killed <-chan error
    )
    {
        var (
            deadRS   = make(chan void)
            killRS   = make(chan error, 1)
            killedRS = make(chan error, 1)
        )
        dead = deadRS
        killed = killedRS
        go killLoop(asyProc, (chan<- error)(killedRS),
            (<-chan error)(killRS), (<-chan void)(deadRS),
        )
        go func(kill chan<- error) {
            var reason error
            select {
            case <-task.Stopped:
                reason = nil
            case <-task.timer.end:
                // XXX specify the actual limit
                reason = reply.Error("Process time limit")
            }
            select {
            case kill <- reason:
            default:
            }
        }((chan<- error)(killRS))
    }

    // Error cases:
    // • killed by timer → "Process time limit (<float seconds>s)"
    // • wait error      → "Server error"
    // • I/O truncated   → "Process output limit (<integer bytes>B)"
    // • nonzero status  → "Execution failed"
    // • other I/O error → "Process I/O error"
    // • no result file  → "No result image"

    var asyErr, asyIOErr, asyProcErr error
    {
        asyState, err := asyProc.Wait()
        close(dead)
        reason := <-killed
        switch {
        case reason != nil:
            asyErr = reason
        case err != nil:
            asyErr = err
        }
        if !asyState.Success() {
            asyProcErr = reply.Error("Execution failed")
        }
    }

    for err := range stdoutDone {
        if asyIOErr == nil {
            asyIOErr = err
        }
    }
    for err := range stderrDone {
        if asyIOErr == nil {
            asyIOErr = err
        }
    }
    if asyErr == nil {
        if reason, ok := asyIOErr.(reply.Error); ok {
            asyErr = reason
        } else if asyProcErr != nil {
            asyErr = asyProcErr
        } else if asyIOErr != nil {
            asyErr = reply.Error("Process I/O error")
        }
    }

    { // send result
        var result []byte
        var err error
        result, err = ioutil.ReadFile(outname)
        if err != nil {
            if asyErr == nil {
                asyErr = reply.Error("No image")
            }
        } else if err := task.conn.SendResult(task.format, result); err != nil {
            log.Print(err)
            return
        }
    }

    if asyErr == nil {
        if err := task.conn.Complete(nil); err != nil {
            log.Print(err)
            return
        }
    } else {
        if err := task.conn.Complete(asyErr); err != nil {
            log.Print(err)
            return
        }
    }
}

func runReader(dest func([]byte) error, abort func() error,
) (*os.File, <-chan error, error) {
    streamRead, stream, err := os.Pipe()
    if err != nil {
        return nil, nil, err
    }
    done := make(chan error, 5)
    go readLoop(streamRead, dest, abort, done)
    return stream, done, nil
}

func readLoop(stream *os.File,
    dest func([]byte) error, abort func() error,
    done chan<- error,
) {
    defer close(done)
    defer stream.Close()
    const (
        bufSize = 1 << 10
        maxSize = 1 << 19
        groupBy = 3e6 // nanoseconds
    )
    var (
        readbuf [bufSize]byte
        sendbuf [1 << 10]byte
        sendval []byte = sendbuf[:0]
    )
    var (
        deadlineSet     = false
        deadlineEnabled = true
        ended           = false
        sent            = 0
    )
    for !ended {
        n, err := stream.Read(readbuf[:])
        sent += n
        if err != nil {
            if deadlineSet && os.IsTimeout(err) {
                deadlineSet = false
                stream.SetReadDeadline(time.Time{})
            } else if err == io.EOF {
                ended = true
            } else {
                done <- err
                ended = true
            }
        } else if sent > maxSize {
            done <- reply.Error(fmt.Sprintf("Process output limit (%dB)", maxSize))
            ended = true
            n -= (sent - maxSize)
            sent = maxSize
            if err := abort(); err != nil {
                done <- err
            }
        } else if deadlineEnabled {
            if !deadlineSet {
                if err := stream.SetReadDeadline(
                    time.Now().Add(time.Duration(groupBy)),
                ); err != nil {
                    if errors.Is(err, os.ErrNoDeadline) {
                        deadlineEnabled = false
                    } else {
                        done <- err
                        deadlineEnabled = false
                    }
                } else {
                    deadlineSet = true
                }
            }
            if deadlineSet {
                sendval = append(sendval, readbuf[:n]...)
                continue
            }
        }
        sendval = append(sendval, readbuf[:n]...)
        if len(sendval) > 0 {
            if err := dest(sendval); err != nil {
                done <- err
                return
            }
        }
        sendval = sendbuf[:0]
    }
}

var (
    timeLimitBase error = reply.Error("Process time limit")
)

func killLoop(proc *os.Process, killed chan<- error,
    kill <-chan error, dead <-chan void,
) {
    defer close(killed)
    select {
    case <-dead:
        return
    case reason := <-kill:
        // TODO maybe kill the whole process group, just in case
        if err := proc.Kill(); err != nil {
            log.Print(err)
        }
        killed <- reason
    }
}

