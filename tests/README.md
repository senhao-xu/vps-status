# Integration and capacity tests

Package-local suites cover protocol and fault-injection behavior. The bounded
end-to-end load command starts a real Control HTTP/WSS stack, isolated control
and telemetry databases, simulated outbound Agents, and a connect-only TCP
target. It verifies live connections, minute host persistence for every Agent,
probe persistence for every assignment, alert evaluation, maintenance,
writable storage/WAL state, and zero dropped telemetry writes.

Fast local smoke run:

```bash
go run ./tests/load --agents 5 --tasks-per-agent 4 --duration 15s
```

The MVP baseline must run on a dedicated Linux host or container constrained to
2 vCPU and 2 GiB. The harness refuses to label a smaller/shorter workload as a
baseline. A recommended command is:

```bash
systemd-run --user --scope -p CPUQuota=200% -p MemoryMax=2G \
  go run ./tests/load --baseline --agents 100 --tasks-per-agent 4 \
  --duration 10m --output load-100x400.json
```

Alternatively run the built harness in a container with `--cpus=2 --memory=2g`.
Retain the JSON, `time -v` output, host kernel, and cgroup limit evidence. The
JSON's `baseline_claim` is true only when the exact 100/400 shape and minimum
duration are selected; the operator must still verify the external cgroup/VM
resource limits. A local smoke result is not evidence that the 2 vCPU/2 GiB
acceptance criterion passed.
