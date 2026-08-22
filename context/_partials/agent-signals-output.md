{{- define "agent-signals-output" -}}
<output_format>
## Output format

```
## Did

- <action> at <path:line>
- <action> at <path:line>

## Tests

- Added: <test name> at <path>
- Existing affected: <test name> at <path>

## Signals

typecheck: ✓ / ✗ (errors)
tests:     ✓ / ✗ (N passed, M failed, K added)
build:     ✓ / ✗ / n/a
lint:      ✓ / ✗ / n/a

## Failed / blocked (if any)

- <what>: <error excerpt>

## Commit

<type>(<scope>): <subject>

<body only when the why is not visible in the diff>
```

If a signal is `n/a`, say why. If a signal is `✗ (could not run: <reason>)`, that's honest — claim nothing.

`## Commit` is the message you would write for this iteration, in the `atomic-git-discipline` format; the orchestrator commits from it. You know why the change is shaped the way it is, and the orchestrator only sees the diff.
</output_format>
{{- end -}}
