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
    "add {
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
    "add {
        filename: <file name>,
        hash: <SHA265 hex file hash>,
    }" b"<file contents>"
    "add {
        filename: <file name>,
        hash: <SHA-265 hex file hash>,
        restore: true,
    }"

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
    "input {}" b"<string>"
        In "interactive+restore" sub-protocol, if the client used "restore"
        feature, it must not send "input" message until at least one "output"
        message is received

Outcoming messages:
    "queue {
        estimate: <float seconds>,
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


### Queue

Each incoming task may have a nominal duration of at most 3s (“fast”),
10s (“medium”), or 30s (“slow”);
a task may also lack specific duration (“default”).

Besides all that, there are “interactive” tasks that provide Asymptote shell.

#### Queue principles

XXX generalize to multiple limits.

Tasks that have a specific duration will run for at most the corresponding
time. Tasks with lower duration will have an advantage in queue, should
the server come under load.

“Default” tasks will be allowed to run for at least the “fast” duration, and
enjoy queue benefits of “fast” tasks, but in the absence of queue (i.e. when
the server is not loaded) they will be initially assigned “slow” duration and
may run for its duration. Should more tasks come in queue, such “default”
tasks may be downgraded to “medium” or “fast” duration, which may stop them
instantly.

“Interactive” tasks are the most vulnerable: they will be denied or halted
anytime it helps to free resources for normal tasks.

There is an upper limit (of at least 1) on the number of concurrently running
“slow” tasks (”slow limit”), a (higher or equal) upper limit on
“medium”-to-“slow” tasks (“medium limit”), and (higher or equal) upper limit
on all running tasks (“fast limit”). Default tasks that are currently assigned
“fast”, “medium”, or ”slow” duration count towards corresponding limits.

#### Queue algorithm

The algorithm cycle (if the queue is not empty) is as follows:
• if a ”slow” task is the first in queue and the “slow limit” is reached:
      • if some “default” tasks are running with “slow” duration, a random one
        of them is downgraded to “medium” duration; if the “medium limit”
        is also reached, this task is downgraded to “fast” duration instead.
      • if the “slow limit” is still reached, all “slow” tasks in queue are
        ignored for the rest of the cycle.
• if any unignored tasks in queue remain, a “medium”-to-“slow” task is
  the first in queue, and the “medium limit” is reached:
      • if some “default” tasks are running with “slow” duration, a random one
        of them is downgraded to “fast”; otherwise, if some “default” tasks
        are running with “medium” duration, a random one of them is downgraded
        to “fast”.
      • if the “medium limit” is still reached, all “medium”-to-”slow” tasks
        in queue are ignored for the rest of the cycle.
• if any unignored tasks in queue remain, and the “fast limit” is reached:
      • if some “default” tasks are running with ”slow” or “medium” duration,
        they all are downgraded to “fast”. This does not impact the
        “fast limit” immediately.
      • if some “interactive” tasks are running, a random one of them
        is halted, its backend connection is shut down, and corresponding
        backend is immediately considered free.
• if at this point there still is an unignored task in the queue, we must be
  able to assign it to a free backend. A ”default” task will be initially
  assigned as large duration as possible without violating limits. After that
  cycle repeats.
• otherwise, cycle is over and we wait for some running task to finish.

“Interactive” tasks never come to the queue. Instead, if there are no free
backends (i.e. “fast limit” is reached), an “interactive” task is denied
immediately. At the same time, if it happens, if some “default” tasks are
running with ”slow” duration, a random one of them is downgraded to “medium”;
otherwise, if some “default” tasks are running with ”medium” duration,
a random one of them is downgraded to “fast”.

#### Queue delay calculation

Clients will receive "queue" messages containing estimate of how much time
they will remain in queue.

Note: Estimate of a client only depends on those who are before it
in the queue (this is not obvious).

Hence, some kind of iterative calculation is in order.

