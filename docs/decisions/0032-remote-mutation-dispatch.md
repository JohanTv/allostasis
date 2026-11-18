---
id: "0032"
title: Remote mutations: one dispatcher, classify then queue or fail loud
status: accepted
created: 2026-11-18
summary: Every mutation toward Plane goes through TrackerService.dispatch, which classifies the failure and either applies, queues (and reports it) or surfaces the error. Unclassified errors are never swallowed.
---

# 0032 · Remote Mutations: One Dispatcher, Classify Then Queue or Fail Loud

Related: [ADR 0031](0031-mutation-input-errors-fail-closed.md), [ADR 0033](0033-ticket-attachments-require-plane.md), [CLI_DESIGN_GUARD](../references/CLI_DESIGN_GUARD.md), [BACKLOG.md](../BACKLOG.md).

## Context

Each remote mutation decided on its own what to do with a failure, and the rules diverged: `create` queued *any* error (so a 400 became a fake `PROJ-LOCAL-n` ticket), `update` discarded every remote error (`_ =`) and wrote only to the local cache with no outbox event, `link add` stored a fabricated local record when Plane failed, `link delete` removed the local row regardless. Whatever a new call site forgot to map was silently lost, and the cache diverged from Plane without notice.

## Decision

1. **One dispatcher.** `TrackerService.dispatch(ctx, remoteMutation{Name, Send, Enqueue})` is the only place that decides what happens to a failed mutation. A call site supplies how to send and, if the operation can be deferred, how to enqueue; it does not inspect errors.
2. **Typed errors, default-deny.** The Plane adapter returns `issue.RejectedError` (400/404/409/422), `issue.AuthError` (401/403) or `issue.TransientError` (network, timeout, 408/429/5xx); `issue.ClassifyRemoteError` maps them, plus the input errors of the domain (`UnknownLabelError`, `UnknownStateError`, `InvalidAttachmentError`, `PreconditionError`). The zero value is `RemoteErrUnknown`: **anything unclassified is surfaced.** A user cancellation (Ctrl+C) is not transient.
3. **Policy.**

   | Outcome | Dispatcher |
   | :--- | :--- |
   | success | `applied` |
   | rejected data | error; retrying cannot fix it |
   | transient, operation deferrable | `queued`, flagged `pending_sync` on the returned item |
   | transient, not deferrable | error |
   | auth, or unknown | error (queueing would hide a configuration problem or a missing mapping) |
   | no client (no credentials) | `queued` if deferrable, else a `PreconditionError` with a hint |

4. **A queued mutation is always reported.** `WorkItem.PendingSync` (not persisted) reaches `--json` (`pending_sync`) and the text output prints a notice. The change is visible in `alo ticket outbox`.
5. **`update_issue` outbox event.** `ticket update` is now applied or queued, never local-only. The payload is the `UpdateIssueInput` as typed (names); the replay resolves state and label names to IDs, runs in order after a pending `create_issue`, and a failure is recorded in the outbox (`last_error`, retried until the attempt limit). A change on a ticket that does not exist in Plane yet is queued and waits for its creation.
6. **State names are validated.** Plane identifies states by ID; an unresolved name used to skip the state change silently. It now returns `UnknownStateError` with the valid states.
7. **Links** use the dispatcher without `Enqueue`: they require Plane and fail instead of writing fake local records; the local copy changes only after Plane confirms.
8. **CLI mapping** (`mutationErrorDetail`): rejected → `user_input`/2 with a hint; auth → `auth`/3; transient → `upstream_service`/5; unknown → `internal`/1.

## Consequences

- A new mutation cannot lose errors by omission: it either supplies `Enqueue` or fails on a transient error, and an unclassified error is shown.
- Behavior change: `create`/`update` against a rejecting or unauthorized Plane now fail instead of queueing. Adapters must return typed errors; an untyped "offline" error is shown, not queued (test doubles model the adapter contract with `issue.TransientError`).
- Known gap, tracked in the backlog: `ticket link list` falls back to the local copy without saying so.
