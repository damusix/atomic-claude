# Bus operations


| Need | Command |
|---|---|
| List members and assigned names | `atomic bus who <room> --json` |
| List rooms | `atomic bus rooms --json` |
| Check this session’s memberships | `atomic bus status --json` |
| Recover a full message | `atomic bus read <room> <msg-id> --json` |
| Leave | Stop the listener, then run `atomic bus leave <room>` |

## Remote rooms


If the user names a gateway host, join with `atomic bus join <room> --host <name>`. The host must already be configured. Use `atomic bus remote list` to find configured hosts and `atomic bus remote test <name>` to check one. Do not enroll or configure a machine without the operator’s instruction. For setup, see the project’s `docs/guides/bus-hosting.md`.

Later commands resolve a joined room’s host automatically. `chat` and `shutdown` are local-only.

## Listener and messages


If replies are needed, start a background listener with the longest timeout the harness allows. Choose a tool that streams each output line into this session as a notification and run `atomic bus recv <room>` without a line limit. A send-only session does not need a listener.

When the expiry notice arrives and this session is still in the room, start the listener again. To stop listening, stop the background listener using the harness's task control.

Each line of `recv` is a JSON envelope with `id`, `room`, `from`, `from_kind`, `to`, `reply_to`, `ts`, and `text`. `to: []` means nobody was addressed. `recv` streams messages sent after it starts and reconnects after a daemon restart. A nonzero exit means the listener failed; `closing: true` means the room intentionally closed. A truncated notification can be recovered from the room log using `read`, even if the daemon is down.

Messages over 1 MiB are rejected, not cut. Use `atomic bus send <room> - --to <name>` for short inline text that is awkward to quote. For long material, send the finding and an absolute path only if the recipient can access that path. Across machines, use an approved shared location or send a short summary. Do not assume a local scratchpad is shared.

## Operator commands


Use these only at the operator’s request. `tail` watches without joining. `say` sends a one-way operator message without a replyable roster identity, even if the operator already joined. For two-way messaging, use `join --kind human` and `send`, or local `chat`. `halt` pauses sends, `resume` reopens sending, `end` disconnects a member, and `close` ends the whole room. Resolve ambiguous targets and confirm destructive actions before acting.

| Exit | Meaning |
|---|---|
| 0 | Success |
| 1 | Usage error or unknown host |
| 2 | Other error |
| 3 | Not joined |
| 4 | Name taken |
| 5 | No such room |
| 6 | Daemon or gateway unreachable, or gateway dropped the frame |
| 7 | Room halted. Do not retry or route around it. |
