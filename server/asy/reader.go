package asy

import (
    "errors"
    "fmt"
    "io"
    //"log"
    "os"
    "time"

    "../server/reply"
)

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
        bufSize               = 1 << 10
        maxSize               = 1 << 19
        groupBy time.Duration = 20e6 // 20ms
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
                done <- err // +1
                ended = true
            }
        } else if sent > maxSize {
            done <- reply.Error(
                fmt.Sprintf("Process reached output limit (%dB)", maxSize),
            ) // +1
            ended = true
            n -= (sent - maxSize)
            sent = maxSize
            if err := abort(); err != nil {
                done <- err // +1
            }
        } else if deadlineEnabled {
            if !deadlineSet {
                if err := stream.SetReadDeadline(
                    time.Now().Add(groupBy),
                ); err != nil {
                    if errors.Is(err, os.ErrNoDeadline) {
                        deadlineEnabled = false
                    } else {
                        done <- err // +1
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
                done <- err // +1
                return
            }
        }
        sendval = sendbuf[:0]
    }
}

