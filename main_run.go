package main

import (
    "net/http"

    "log"
)


func main() {
    runner := runner{}
    runner.init()
    server := server{handler: &runner}
    mux := http.NewServeMux()
    mux.Handle("/asy", server)
    err := (&http.Server{
        Addr:    "localhost:8080",
        Handler: mux,
    }).ListenAndServe()
    log.Fatal(err)
}

