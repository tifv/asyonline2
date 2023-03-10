### Rename Go modules (TODO)

"handler" or "parser" or "reader"
• module that listens to websocket connection and parses its messages and
  passes options and files to "asy" or elsewhere.

"responder" or "writer"
• module that takes output and results of compilation from "asy" or elsewhere
  and serializes and writes them to the websocket connection

"asy" or "asyrun" or "asysrv", "task" or "asytask"
• module that writes files and starts Asymptote and gets the result

"enqueuer"
• module that takes parsed options and files and puts them in some sort of
  queue (like redis-based)
• module that takes output and results of compilation from a backend connection
  and passes them to the "server"/"writer"

"scheduler" or "referee"
• module that watches for changes in the queue (like redis-based) and moves
  items around to ensure fair queue order.

"backend"
• module that reads options and files from queue (like redis-based) and passes
  them to "asy".
• module that takes output and results of compilation from "asy" and passes
  them to "enqueuer" (either via redis or directly).


### Other TODO

Switch to either of
https://pkg.go.dev/github.com/gorilla/websocket
https://pkg.go.dev/nhooyr.io/websocket

Replace Stopper with context.Context

Implement timer (in "asy") as a Context-factory.

Concede to using tabs.

go run -race
go vet
golint?

<!-- vim: set tw=79 fo-=l : -->
