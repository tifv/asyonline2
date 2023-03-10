### Server (universal)

WebSocket sub-protocols at "/asy"
• asyonline.asy
• asyonline.asy+restore

WebSocket sub-protocols at "/asy/interactive"
• asyonline.asy.interactive
• asyonline.asy.interactive+restore

Here
  • "restore" means that server accepts "restore" parameter to "add" messages,
    restoring recent source files instead of rereceiving them;
  • "interactive" means that server will run an Asymptote shell.

If any protocol is supported by server, then all “lesser” protocols also
should be supported.

#### Protocol, stage 1

Incoming messages:
    "input {
        filename: <file name>,
    }" b"<file contents>"
        filename must have .asy extension
        (though may allow other extensions in future)
    "options {
        duration: <float seconds>,
            optional, should be one of 3.0, 10.0, or 30.0
            default is server-decided
            server may impose an additional limit on duration
            must be absent if sub-protocol is "interactive"
        format: <"svg"/"pdf"/"png">,
            optional, default is "svg"
        stderrRedir: false
            optional, default is true
        verbosity: <0/1/2/3>,
            optional, default is 0
    }"

Also incoming messages if the sub-protocol contains "restore":
    "input {
        filename: <file name>,
        hash: <SHA265 hex file hash>,
    }" b"<file contents>"
    "input {
        filename: <file name>,
        hash: <SHA-265 hex file hash>,
        restore: true,
    }"

Incoming messages in "interactive" sub-protocol:
    "input {
        stream: stdin,
    }" b"<string>"

Outcoming messages:
    "deny {error: <message>}"
        indicates that an error occured before actually handling the task
        (like an error in arguments, or an overloaded server)

#### Protocol, stage switching

Incoming message
    "start {
        main: <filename>
            skipped/ignored if sub-protocol is "interactive"
    }"
switches stage 1 to stage 2.

#### Protocol, stage 2

Outcoming messages if sub-protocol contains "restore"
    "missing [{
        filename: <file name>,
        hash: <SHA265 hex file hash>,
    }, …]"
switches back to stage 1, can be sent only once;
suggests that all missing files must be readded.

In "interactive+restore" sub-protocol, if the client used "restore" feature,
the server must send possibly empty "output" message (see below) to inform
the client that all files were restored successfully.

Incoming messages:
    "options {duration: <float seconds>}"
        further limit exection duration that was set earlier

Incoming messages in "interactive" sub-protocol:
    "input {
        stream: stdin,
    }" b"<string>"

Outcoming messages:
    "status {
        queue: {estimate: <float seconds>},
        announcement: <text>,
    }"
    "result {format: <"svg"/"pdf"/"png">}" b"<image contents>"
    "output {stream: <"stdout"/"stderr">}" b"<output>"
        empty output should be sent to indicate the start of the process
    "complete {
        error: <message>,
            optional
    }"
    "deny {error: <message>}"
        see stage 1

Error messages may include:
    • "Execution aborted due to the time limit (<float seconds>s)"
    • "Execution aborted due to the output limit (<integer bytes>B)"
    • "Execution failed"
    • "No image output"

When the task completes or is denied, connection is closed.

Closing connection in any case aborts execution and clears residual files.


### Server Announcements

Response to "/asy/status"
    "status" : {
      "announcement" : <text>,
        // like a maintenance warning, dunno
    }

(App will request this status on load.)
Also, announcement may arrive via the websocket protocol without additional
request.

### JSON TODO

Rewrite the protocol into pure JSON.

Incoming messages:
    {
      "input" : [
        {
          "filename" : "<filename>",
            // <filename> must have .asy extension
            // (though we may allow other extensions in future)
          "blob" : <i1>,
            // "blob" indicates that binary messages will follow this message.
            // The number of binary messages must equal the number of elements of
            // "input" list that have a "blob" field.
            // "blob" fields must all be distinct; <i1> is a 0-based index in
            // the list of messages that corresponds to this particular input.
        }, {
          "filename" : "<filename>",
          "hash" : "<SHA256 hex file hash>"
            // "hash" will be checked if "restore" sub-protocol was negotiated.
            // If "hash" is absent, the fill will not be remembered.
          "blob" : <i2>,
        }, {
          "filename" : "<file name>",
          "hash" : "<SHA256 hex file hash>",
          "restore" : true,
            // only allowed if "restore" sub-protocol was negotiated.
            // the "hash" must also be present, "blob" must not be present
        }, {
          "stream" : "stdin",
          "blob" : <i3>,
          // only allowed if "interactive" sub-protocol was negotiated.
        },
        …
      ],
      "options" : {
        "duration" : <float seconds>,
          // optional, should be one of 1.0, 3.0, or 10.0
          // default is server-decided
          // server may impose an additional limit on duration
          // must be absent if sub-protocol is "interactive"
        "format": <"svg"/"pdf"/"png">,
          // optional, default is "svg"
        "stderrRedir": false
          // optional, default is true
        "verbosity": <0/1/2/3>,
          // optional, default is 0
          // affects the amount of output from asy, not the protocol itself
      }
      "start" : {
        "main" : <filename>,
      }
    }
After "start", only "options.duration" updates are accepted by the server.

Outcoming messages (only after "start"):
    {
      "missing" : [
        // reverts the protocol back to pre-"start" stage
        // client should resend all missing files and resend "start"
        // (otherwise server will just drop them)
        {
          "filename" : "<filename>",
          "hash" : "<SHA256 hex file hash>"
        },
      ],
      "output" : [
        {
          "format" : <"svg"/"pdf"/"png">,
            // should be same as in the options
          "blob" : <i1>,
            // subsequent binary message index, similar to "input"
        }, {
          "stream" : <"stdout"/"stderr">,
          "blob" : <i2>,
        },
        …
      ],
      "status" : {
        "queue" : {
          "estimate" : <float seconds>,
        }
        "announcement" : <text>,
          // like a maintenance warning, dunno
      }
      "ok" : 1,
      "error" : "<error message>",
        // exactly one of "ok" or "error" should ever be sent.
        // after that, server will close the connection
    }

Error messages may include:
    • "Execution aborted due to the time limit (<float seconds>s)"
    • "Execution aborted due to the output limit (<integer bytes>B)"
    • "Execution failed"
    • "No image output"

When the task completes or is denied, connection is closed.

Closing connection in any case aborts execution.

<!-- vim: set tw=79 fo-=l : -->
