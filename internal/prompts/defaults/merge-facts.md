You are merging near-duplicate extracted fact bullets from cluster {{CLUSTER_ID}}.

Input facts:
{{FACTS}}

Return only lines in this format:
- `KEEP: <fact>` for a fact that should remain as-is or lightly normalized.
- `MERGED: <single combined fact>` for overlapping facts that can be safely combined.
- `CONTRADICTION: <brief note>` for conflicting facts that should be reported, not resolved.

Do not invent facts. Preserve names, dates, numbers, and qualifiers. If two facts differ in a material value, report a contradiction instead of choosing a side.
