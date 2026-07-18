You are preparing a document-context header that will be shown to a fact-extraction model alongside individual chunks of this document.

From the heading outline and excerpt below, produce a compact header (at most 200 tokens) with exactly these parts:

1. TITLE: one line naming the document.
2. SYNOPSIS: 2-3 sentences describing what the document covers.
3. GLOSSARY: one line per key entity or abbreviation likely to recur across the document, formatted `TERM — expansion or one-line identification`. Include people, organizations, products, and abbreviations. Omit this section if nothing qualifies.

Rules:
- Use only information present in the headings/excerpt; never invent.
- Markdown only. No commentary, no preamble, no code fences.

HEADINGS:
{{HEADINGS}}

EXCERPT:
{{EXCERPT}}
