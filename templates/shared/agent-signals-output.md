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
```

If a signal is `n/a`, say why. If a signal is `✗ (could not run: <reason>)`, that's honest — claim nothing.
</output_format>
{{- end -}}
