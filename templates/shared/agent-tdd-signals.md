{{- define "agent-tdd-signals" -}}
3. **TDD**:
    - For new behavior: write failing test first, run it, confirm it fails for the right reason (not a syntax error). Implement. Run again, confirm green.
    - For bug fixes: write a test that reproduces the bug (fails on current code), then fix, then confirm green.
    - For pure docs/config/comment/typo changes: skip TDD, state why.
4. Run quality signals. Detect commands from the project (`package.json` scripts, `Makefile`, `Cargo.toml`, `pyproject.toml`, etc.): typecheck, test, build, lint.
{{- end -}}
