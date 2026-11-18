---
id: "0033"
title: Ticket attachments: signed-upload protocol, no outbox, no simulated success
status: accepted
created: 2026-11-18
summary: alo ticket attachment add|list|delete|replace uploads real files to Plane CE through its signed-storage protocol; it never queues and never reports a success it did not achieve.
---

# 0033 · Ticket Attachments: Signed-Upload Protocol, No Outbox, No Simulated Success

Related: [ADR 0032](0032-remote-mutation-dispatch.md), [CLI_DESIGN_GUARD](../references/CLI_DESIGN_GUARD.md), [ADR 0025](0025-json-read-contract.md).

## Context

The Go port lost attachments: `TrackerService.AddAttachment` was a stub that never read the file or called Plane, fabricated `SizeBytes: 1024` and an `AssetURL`, and no command exposed it. The legacy Python CLI had `attachment add|list|delete|replace` (tag `archive/python-original`, `plane_cli/attachments.py`).

## Decision

1. **Grammar.** `alo ticket attachment add <TICKET> <FILE>...`, `list <TICKET>`, `delete <TICKET> <ATTACHMENT-ID>`, `replace <TICKET> <ATTACHMENT-ID> <FILE>` (sub-resource under `ticket`, like `link` and `comment`). `create --attach` and `get --include attachments` are **not** restored: they cross command domains ([CLI guard](../references/CLI_DESIGN_GUARD.md), filters 1-4) and compose from `create` then `attachment add`.
2. **Protocol (Plane CE).** `POST …/issue-attachments/` (name, type, size) returns `asset_id` and a signed `upload_data` → multipart `POST` of the file to that URL (signed fields first, file last) → `PATCH …/{asset_id}/` with `is_uploaded: true`. If the storage upload or the confirmation fails, the pending asset is deleted; if that cleanup also fails, the error says so.
3. **The Plane API key never reaches the storage URL.** It is a third-party endpoint (S3/MinIO); only the signed fields authenticate it. Covered by a test.
4. **No outbox.** A file can change or disappear before a sync, so an attachment is not deferrable (ADR 0032): without a connection, or on any failure, the command fails. Nothing is recorded as uploaded unless Plane confirmed it.
5. **Validate first.** Every path is checked before contacting Plane (regular file, ≤ 50 MB, extension not `.sql`), so a multi-file upload does not stop halfway because of an invalid file; `delete`/`replace` validate that the id is a UUID. A failure midway returns what was already uploaded.
6. **`replace`** checks that the old attachment exists, uploads the new one and only then deletes the old; if the deletion fails it reports both ids.
7. **`list` states its source.** Plane is authoritative: when it answers, the local copy is reconciled (deleted attachments dropped, unless Plane announces more pages). On a transient failure it returns the local copy with `source: "cache"` and a note; auth and unknown errors are not hidden behind the cache.
8. **Local copy.** The existing `issue_attachments` table is a convenience copy, written only after Plane confirms.

## Consequences

- Attachments need connectivity and credentials; the failure is explicit, with a category and exit code per class (ADR 0032).
- Streaming is not implemented: the file (≤ 50 MB) is read into memory because the signed POST needs a `Content-Length`.
- Verified end to end against a fake Plane in `cmd/alo` tests and manually against Plane CE (upload, list, replace, delete).
