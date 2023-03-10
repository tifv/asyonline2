### Queue

XXX generalize to multiple limits.

Each incoming task may have a nominal duration of at most 3s (“fast”),
10s (“medium”), or 30s (“slow”);
a task may also lack specific duration (“default”).

Besides all that, there are “interactive” tasks that provide Asymptote shell.

#### Queue principles

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


### Redis queue

#### Database schema

Tasks

int    task.counter (ID generator)
hash   task:`ID` (task options)
hash   task:`ID`:files (task source files)
list   task:`ID`:backend (backend address)
list   task:`ID`:queue.estimate (float seconds)

Queue

list   queue.incoming (task IDs)
zset   queue (task IDs with scores increasing in incoming order)
stream queue.outcoming (task IDs)
set    queue.processing (task IDs)
list   queue.finished (task IDs)
hash   queue.executing (task IDs to durations)

Backends

int    backend.counter (ID generator)
set    backend.set (backend IDs)
list   backend.updated (backend IDs)
string backend:`ID` (zero-length string)


#### Queue server (task)

Generate an ID by incrementing "task.counter".

Put task details in "task:`ID`" hash, and files in "task:`ID`:files" hash.
Both keys expire in 600 seconds.

Append `ID` to "queue.incoming" list.

Watch "task:`ID`:queue.estimate" list.
Get an estimate of how long we have to wait in the queue, forward it to the
client.

Watch "task:`ID`:backend" list. Get an address from it.

Open a websocket connection to the backend address; send duration updates,
receive responses and forward them to the client.

Delay "task:`ID`:files" from expiring util the backend address is received.
Delay "task:`ID`" from expiring until the connection to backend is stopped
or the task is aborted.

If the task is aborted before backend address is delivered, clear the task
keys.
If the task is aborted after backend address is delivered, just close the
backend connection.
When the connection to the backend is closed or the task is aborted, delete
"task:`ID`" and other corresponding keys.

#### Scheduler server

Upon startup, clear "queue.durations".

Watch "queue.incoming" list.
Take `ID` string from it.
Append `ID` to "queue" zset with score = current max score + 1.
Add the task to "queue.durations" hash, setting its value to the duration of
the task (get it from "task:`ID`" hash "duration" field).

Maintain a series of limits on the number of tasks exceeding certain execution
duration. The fastest limit must be zero and equal to the number of backends.

Maintain a series of counters corresponding to the limits (initially equal to
the limits).

Watch "queue.finished" list.
Take `ID` from it and restore counters based on its value in "queue.durations".
Clear `ID` from "queue.durations".

Watch "backend.updated" list.
If a backend arrives, increate limits, update counters accordingly (if they
were set as percentage).

Allocate a 1 from counters (starting from the fastest) if possible. Either find
the first task from the "queue" that satisfies allocated limits, or watch for
an incoming task, or watch for a finished task.

If the task in "queue" is found, take it from there and put in
"queue.outcoming" stream as {"task" : `ID`}.

Periodically monitor for long-pending tasks in "queue.outcoming"; consider them
aborted.

Periodically monitor tasks in "queue.processing". If the "task:`ID`" key
is absent, consider the task aborted and remove it (restoring the counters).

Periodically monitor backends in "backend.set". If a corresponding
"backend:`ID`" is absent, remove the backend and update 

XXX queue estimates

#### Backend server

On startup:
Generate an `ID` by incrementing "backend.counter".
Add `ID` to "backend.set" set
Add `ID` to "backend.updated" list.
Set "backend:`ID`" as empty string, expire after 600 seconds.
Delay it from expiring.

On exit, try to:
Delete the "backend:`ID`" key.
Add `ID` to "backend.updated".
Remove self from "backend.set".
If there was a task executing, move it to "finished".

Cycle:

Prepare a more-or-less unique URL.
Watch "queue.outcoming" stream as "backend" consumer group.
Take a {task: `ID`} from it.
Atomically:
    Acknowledge the stream message.
    Delete the stream message.
    Put `ID` in "queue.processing" set.
    Load options from "task:`ID`" hash (check that it exists).
    Put a websocket URL into "task:`ID`:backend" list, set expiration on it.
Load files from "task:`ID`:files" hash, delete the key.
Start executing the task.
Accept a connection to the URL.
Sends results of the task execution via the connection.
Close the connection.

Repeat.

<!-- vim: set tw=79 fo-=l : -->
