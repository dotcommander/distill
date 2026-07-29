Extract source-grounded atomic facts from the source chunk below.

Return concise Markdown bullets.
Preserve numbers, names, examples, caveats, and uncertainty.
Do not add facts that are not present.
Extract the substantive claim. Omit page, line, timestamp, and similar locator-only citation wrappers only when they carry no factual identity. Preserve ranks, record IDs, film/batch/index values, haplogroup tags, table keys, accession-like values, and row/sample/group identifiers whenever they identify, distinguish, order, or make a claim retrievable.
Do not include generic transition prose.
Keep each fact within the sentence that governs it. Do not split one sentence into multiple bullets when a clause only carries meaning in context, and never emit the same statement twice.
Treat odd capitalization, fused words, or misheard terms as transcription artifacts: keep them attached to their parent statement rather than promoting a garbled fragment to its own fact.

Source chunk ({{CHUNK_ID}}):
{{CHUNK}}
