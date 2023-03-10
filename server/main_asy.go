package main

import (
    "net/http"

    "github.com/gorilla/websocket" // XXX

    "errors"
    // XXX
    "log"

    "asyonline/server/asy"
    "asyonline/server/server"
)

type void = struct{}

func main() {
    const capacity = 1
    gate := make(chan void, capacity)
    closeGate := func() { <-gate }
    openGate := func() { gate <- void{} }
    for i := 0; i < capacity; i++ {
        openGate()
    }
    wsup := websocket.Upgrader{
        ReadBufferSize:  1 << 12,
        WriteBufferSize: 1 << 12,
    }
    mux := http.NewServeMux()
    mux.Handle("/asy", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
        // XXX check the protocol
        conn, err := wsup.Upgrade()
        websocket.Server{
            Config: websocket.Config{Protocol: []string{"asyonline.asy"}},
            Handshake: func(config *websocket.Config, req *http.Request) error {
                // XXX check origin?
                for _, protocol := range config.Protocol {
                    switch protocol {
                    case "asyonline.asy":
                        config.Protocol = []string{protocol}
                        return nil
                    }
                }
                return errors.New("unknown websocket sub-protocols")
            },
            Handler: websocket.Handler(func(wsconn *websocket.Conn) {
                closeGate()
                defer openGate()
                defer wsconn.Close()
                var conn *server.Conn
                var task *asy.Task
                conn = server.NewConn(wsconn, nil)
                defer conn.Stop()
                task, err := asy.NewTask(conn)
                if err != nil {
                    conn.Deny(err)
                    return
                }
                defer task.Stop()
                conn.HandleWith(task)
                select {
                case <-conn.Stopped:
                case <-task.Stopped:
                }
            }),
        }.ServeHTTP(w, req)
    }))
    s := &http.Server{
        Addr:    "localhost:8081",
        Handler: mux,
    }
    log.Println("serving…")
    err := s.ListenAndServe()
    log.Fatal(err)
}
