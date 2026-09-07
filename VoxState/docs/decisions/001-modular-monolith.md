# ADR 001: Start as a Modular Monolith, Not Microservices

## Status
Accepted — M0

## Context

VoxState needs to coordinate several concerns that are conceptually
distinct: voice transport (LiveKit), LLM reasoning/tool-calling, state
versioning, task lifecycle and cancellation, stale-result policy
enforcement, and event logging. It would be easy to justify splitting these
into separate services, since they're separate *concerns*.

But the project's actual goal is to demonstrate a specific correctness
problem — that stale task results must not influence a spoken response, and
that interruptions must reliably cancel in-flight work — under hackathon
time constraints.

## Decision

Build VoxState as a single Go process (a modular monolith): one binary, with
internal packages (`internal/api`, `internal/state`, `internal/events`,
`internal/tasks`, `internal/tools`, `internal/agent`, `internal/policy`)
enforcing separation of concerns through Go package boundaries and
interfaces, not network boundaries.

PostgreSQL is the one external stateful dependency. Redis, message queues,
or additional services are not introduced unless a later milestone produces
a concrete, demonstrated need (see `docs/ROADMAP.md` — none of M0–M9
currently require it).

## Alternatives considered

**Microservices (e.g. separate state service, task service, agent
service, communicating over gRPC/HTTP/queues).**
Rejected. This would introduce network partial-failure and cross-service
latency as *additional* sources of staleness/race conditions, on top of the
ones we're deliberately trying to demonstrate. That would muddy the demo:
an audience watching a stale result get rejected should see that it's
because of state versioning logic, not because two services briefly
disagreed over a flaky network call. It also multiplies deployment and
local-dev complexity for no corresponding benefit at this scope.

**Serverless functions per concern.**
Rejected for similar reasons, plus added cold-start latency working against
the "realtime" requirement, and awkward fit for LiveKit's persistent
connection model.

**Fully flat, unstructured single package.**
Rejected. Without internal package boundaries, the state/task/policy
separation that the whole architecture depends on would erode under time
pressure — it's easy to reach into another concern's internals when
everything is one package. Explicit internal packages with narrow
interfaces keep the boundaries real even inside one process.

## Consequences

- Local development is one `go run`, one Postgres container — fast
  iteration, which matters under hackathon time pressure.
- Internal package boundaries (`internal/state`, `internal/policy`, etc.)
  must be respected by convention and code review discipline, since Go
  won't stop one internal package from importing another's internals the
  way a network boundary would. This is an accepted tradeoff.
- If VoxState ever needed to scale specific components independently (e.g.
  the Tool Orchestrator running many long tasks), the internal package
  boundaries are drawn so that a future split is possible without a full
  rewrite — but doing that split now would be solving a problem we don't
  have yet.
- Redis and additional infrastructure are deferred, not ruled out — revisit
  this ADR if a specific milestone identifies a concrete need (e.g. if task
  concurrency across multiple backend instances is ever required).
