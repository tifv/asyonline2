package main

import (
    "net/http"
    "golang.org/x/net/websocket"

    "errors"
    "log"

    "./server"
    "./queue"
)

type void = struct{}

func main() {
    q := queue.NewQueue([]string{"localhost:8081"})
    mux := http.NewServeMux()
    mux.Handle("/asy", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
                var conn *server.Conn
                var task *queue.Task
                conn = server.NewConn(wsconn, nil)
                defer conn.Stop()
                task, err := q.NewTask(conn)
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
    log.Println("serving…")
    err := (&http.Server{
        Addr:    "localhost:8080",
        Handler: mux,
    }).ListenAndServe()
    log.Fatal(err)
}

