---
id: artifact-templates-f-8
title: doc-impact-why has 1 consumer (commit-flow only)
created: "2026-05-23"
origin: |
    docs/spec/artifact-templates.md, iter 6 investigation (CP-5)
severity: risk
review_by: "2026-07-22"
status: open
file: templates/shared/doc-impact-why.md
---

SC 11 requires >=2 consumers for small partials. doc-impact-why is consumed only by commit-flow. squash-flow has a flow-specific inline variant whose step numbers ("step 4...step 8") differ from the partial's commit-flow-specific text ("step 5...step 6"). Using the partial in squash-flow would render the wrong step numbers, breaking leaf-verb byte-equality. Resolution options for a future text-harmonization pass: (a) make the tagline step-number-agnostic and consume from both flows, or (b) accept 1-consumer permanently. Deferred.

Origin: docs/spec/artifact-templates.md, iter 6 investigation (CP-5).
