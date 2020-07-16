### Asymptote server (kinda backend)

Source location: runasy/

Server supports WebSocket-based protocol at "/".
Only 1 simultaneous connection is supported

Incoming messages may include
    "add {
        filename: <file name>,
        main: true/false
    }" b"<file contents>"
        filename must have .asy extension;
        may allow other extensions
    "options {
        interactive: <bool>,
            default is false
        timeout: <integer milliseconds>,
        format: <"svg"/"pdf"/"png">,
            default is "svg"
        stderr: "separate" or "stdout",
            default is "stdout"
        verbosity: <0/1/2/3>,
            default is 0
    }"
        (all options are optional, lol)
then, once
    "run"
then
    "options {timeout: <integer milliseconds>}"
        to further limit execution time
or, if interactive
    "input" b"<string>"
Closing the connection aborts execution and clears residual files.

Outcoming messages may include
    "result {format: <"svg"/"pdf"/"png">}" b"<image contents>"
    "output {stream: <"stdout"/"stderr">}" b"<output>"
    "complete {
        success: true/false,
        error: <message>,
            optional
        time: <integer milliseconds>,
            optional, how long did the execution take
    }"
When the task completes, connection is closed.

Error messages may include:
    • "Execution aborted due to the time limit (<integer milliseconds>ms)"
    • "Execution aborted due to the output limit (<integer bytes>B)"
    • "Execution failed with code <return code>"
    • "No image output"

### Queue server (kinda fronend)

Source location: queue/

Server that supports WebSocket-based protocol at "/asy"

XXX make interactive an option

Incoming messages may include
    "add {
        filename: <file name>,
        main: true/false,
        hash: <SHA265 hex file hash>,
    }" b"<file contents>"
        filename must have .asy extension
        (may allow other extensions in future)
    "add {
        filename: <file name>,
        main: true/false,
        hash: <SHA-265 hex file hash>,
        restore: true,
    }"
        only works on recently uploaded files
    "options {
        interactive: <bool>,
            default is false
        timeout: <integer milliseconds>,
            timeout should be one of 3000, 10000, or 30000
        format: <"svg"/"pdf"/"png">,
            default is "svg"
        stderr: "separate" or "stdout",
            default is "stdout"
        verbosity: <0/1/2/3>,
            default is 0
    }"
        (all options are optional, lol)
then, once
    "run"
after interactive "run"
    "input" b"<string>"
Closing the connection aborts execution.

Outcoming messages may include
    "missing [{
        filename: <file name>,
        hash: <SHA265 hex file hash>,
    }, …]"
        (this is a request for reupload, giving a second chance to "run")
    "queue {
        passed: true/false,
        estimate: <integer milliseconds>,
            optional
    }"
    "result {format: <"svg"/"pdf"/"png">}" b"<image contents>"
    "output {stream: <"stdout"/"stderr">}" b"<output>"
    "complete {
        success: true/false,
        error: <message>,
            optional
        time: <integer milliseconds>,
            optional, how long did the execution take
    }"
    "denied {error: <message>}"
        indicates that error occured before executing any asymptote code
        like an error in arguments, or full queue in response to interactive
        request
When the task completes or is denied, connection is closed.

### Queue

Each incoming task may have a nominal timeout of at most 3s (“fast”),
10s (“medium”), or 30s (“slow”);
a task may also lack specific timeout (“default”).

Besides all that, there are “interactive” tasks that provide Asymptote shell.

-- Queue principles --

XXX generalize to multiple limits.

Tasks that have a specific timeout will run for at most the corresponding
time. Tasks with lower timeout will have an advantage in queue, should
the server come under load.

“Default” tasks will be allowed to run for at least the “fast” timeout, and
enjoy queue benefits of “fast” tasks, but in the absence of queue (i.e. when
the server is not loaded) they will be initially assigned “slow” timeout and
may run for its duration. Should more tasks come in queue, such “default”
tasks may be downgraded to “medium” or “fast” timeout, which may stop them
instantly.

“Interactive” tasks are the most vulnerable: they will be denied or halted
anytime it helps to free resources for normal tasks.

There is an upper limit (of at least 1) on the number of concurrently running
“slow” tasks (”slow limit”), a (higher or equal) upper limit on
“medium”-to-“slow” tasks (“medium limit”), and (higher or equal) upper limit
on all running tasks (“fast limit”). Default tasks that are currently assigned
“fast”, “medium”, or ”slow” timeout count towards corresponding limits.

#### Queue algorithm

The algorithm cycle (if the queue is not empty) is as follows:
• if a ”slow” task is the first in queue and the “slow limit” is reached:
      • if some “default” tasks are running with “slow” timeout, a random one
        of them is downgraded to “medium” timeout; if the “medium limit”
        is also reached, this task is downgraded to “fast” timeout instead.
      • if the “slow limit” is still reached, all “slow” tasks in queue are
        ignored for the rest of the cycle.
• if any unignored tasks in queue remain, a “medium”-to-“slow” task is
  the first in queue, and the “medium limit” is reached:
      • if some “default” tasks are running with “slow” timeout, a random one
        of them is downgraded to “fast”; otherwise, if some “default” tasks
        are running with “medium” timeout, a random one of them is downgraded
        to “fast”.
      • if the “medium limit” is still reached, all “medium”-to-”slow” tasks
        in queue are ignored for the rest of the cycle.
• if any unignored tasks in queue remain, and the “fast limit” is reached:
      • if some “default” tasks are running with ”slow” or “medium” timeout,
        they all are downgraded to “fast”. This does not impact the
        “fast limit” immediately.
      • if some “interactive” tasks are running, a random one of them
        is halted, its backend connection is shut down, and corresponding
        backend is immediately considered free.
• if at this point there still is an unignored task in the queue, we must be
  able to assign it to a free backend. A ”default” task will be initially
  assigned as large timeout as possible without violating limits. After that
  cycle repeats.
• otherwise, cycle is over and we wait for some running task to finish.

“Interactive” tasks never come to the queue. Instead, if there are no free
backends (i.e. “fast limit” is reached), an “interactive” task is denied
immediately. At the same time, if it happens, if some “default” tasks are
running with ”slow” timeout, a random one of them is downgraded to “medium”;
otherwise, if some “default” tasks are running with ”medium” timeout,
a random one of them is downgraded to “fast”.

#### Queue delay calculation

Clients will receive "queue" messages containing estimate of how much time
they will remain in queue.

Note: Estimate of a client only depends on those who are before it
in the queue (this is not obvious).

Hence, some kind of iterative calculation is in order.

