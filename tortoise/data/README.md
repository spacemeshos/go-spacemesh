Tortoise execution traces
===

Traces can be collected from a local or cloud environment, and they are meant to be used as a regression tests. Good source of traces are executions from system tests (see ./systest directory) that we run in the cloud.

To download the trace:
- go to the grafana
- find a pod that you are interested in
- query tracer. example
> {namespace="test-kaih", pod="smesher-10-6f86f487c-knfqm"} | json | logger="tracer"
- click download, and postprocess file to reduce amount of data. example
> cat Explore-logs-2023-06-01\ 15\ 59\ 49.json |  jq -c '.[].line | fromjson | {"t": .t, "o": .o}' &> ~/go-spacemesh/tortoise/data/partition_50_50_split_other_side.json

Note that whole execution might be very large, and you will have to tweak period to download trace in one go. Alternatively you can use [logcli](https://grafana.com/docs/loki/latest/tools/logcli/).

Lists of traces
===

<Add a short note a
bout trace and how you collected it here>

| name | source |
| ---  | ---    |
| partition_70_30_ex_1 | {namespace="test-ugma", pod="smesher-2-84564c8cc-rzl7h"} |
| partition_70_30_ex_2 | {namespace="test-ugma", pod="smesher-23-8665c485bc-4m8j2"}|
| failed_nodes_ex_1 | {namespace="test-tzch", pod="smesher-45-5c6856cf69-rmmjt"} |
| partitin_50_ex_1 | {namespace="test-bxvs", pod="smesher-3-6d49fb7cb8-dqd78"} |
| partition_50_ex_2 | {namespace="test-bxvs",pod="smesher-27-7c5dff6888-q7sl5"}|


How to run?
===

All traces in the `./tortoise/data` directory will be executed as a part of automated tests. Additionally there is a command line tool to debug trace interactively, it can be used with [delve](https://github.com/go-delve/delve).

In the example below breakpoint will be placed after executing event. Debug logger will allow you to see what happened. Also you can place a breakpoint wherever you want and recompile `trace`.

> dlv exec ./trace -- -breakpoint -level=debug ./tortoise/data/partition_50_50_long.json