package queue

import (
    "golang.org/x/net/websocket"
)

type backend struct {
    addr string
}

// create and return websocket connection
// closing the connection is the responsibility of the caller
func (b *backend) Dial() (*websocket.Conn, error) {
    conn, err := websocket.Dial(
        "ws://"+b.addr+"/asy",
        "asyonline.asy",        // protocol
        "http://localhost/asy", // origin
    )
    if err != nil {
        return nil, err
    }
    return conn, nil
}

type backendPool struct {
    input  chan<- *backend
    output <-chan *backend
}

func newBackendPool() *backendPool {
    // TODO some control mechanisms to remove and add backends from the pool
    backends := make(chan *backend)
    return &backendPool{backends, backends}
}

