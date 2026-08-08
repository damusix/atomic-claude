---
id: bus-daemon-room-name-validation
title: Bus daemon accepts path-shaped room names (Hub.Join length-check only)
created: "2026-08-08"
origin: |
    serve-bus-chat autopilot CP2 review
kind: finding
severity: risk
review_by: "2026-10-07"
status: open
---

Hub.Join validates room length only; Append derives RoomLogPath(home, room) from it, so a programmatic client could register a room whose name path-escapes the rooms dir (bounded to a .log write). Serve's /api/bus/* and the bus read verb both guard client-side (2026-08-08); the daemon-side guard in Hub.Join is the durable fix. Files: atomic/internal/bus/room.go (Join), roomlog.go (Append).
