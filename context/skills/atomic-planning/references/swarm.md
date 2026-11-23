# Independent-lens method

Accept a written artifact by Markdown path or topic. Resolve paths within the repository and reject missing, non-Markdown, or outside-repository paths. For a topic, find matching design or spec files and ask the user to select if ambiguous. With no target, ask which artifact to challenge.

Read the artifact and its source material. Select 3–6 independent lenses with stakes evidenced by the artifact or sources, such as a user, maintainer, operator, domain expert, or tester. Ask about an unknown exposure, audience, or consequence when it changes the roster. Name the selected lenses and their stakes; omit irrelevant perspectives.

Create a review scratchpad with `atomic scratchpad new <slug> --purpose review`. Dispatch each lens independently with the target, its role, and relevant sources. Do not share other lenses' findings. Each lens reports claims, severity, source or reasoning evidence, questions, and assumptions. Wait for all lenses before synthesis. If independent agents are unavailable, state that sequential perspectives are not independent and ask whether the user wants that weaker review instead.

Check objectively verifiable disputes against sources. Report conflicts and the decisions they require, independently reinforced findings, single-lens findings, shared assumptions, and missing perspectives. Cite each finding, omit unsupported or duplicate claims, and leave human tradeoffs unresolved. Do not modify the target or code. The user may choose to revisit a lens or incorporate decisions through `/atomic-plan`.
