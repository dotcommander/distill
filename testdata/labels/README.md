# Label-task fixtures

Gold fixtures for `distill-eval models label`. Each file is a closed-taxonomy, single-label
gold set scored by deterministic exact match (no LLM judge).

## Schema

    {
      "task": "classification | sentiment",
      "allowed_labels": ["label-a", "label-b", "..."],
      "cost_per_mtok": { "model/slug": 0.42 },   // optional; omit for no cost column
      "items": [
        { "id": "unique-id", "text": "the input to classify", "gold_label": "label-a" }
      ]
    }

Loader rules (`internal/labelscore.LoadFixture`): task and allowed_labels
non-empty; item ids unique and non-empty; every gold_label (after
lowercase/punctuation normalization) must be in allowed_labels.

## Files

- `sentiment.json` — 21 items, 3 classes (positive/negative/neutral), with
  sarcasm, litotes, and mixed-sentiment discriminators.
- `classification.json` — 22 items, 5 topic classes, with cross-domain
  discriminators (e.g. a fitness-app funding round → business).

## Run

See `runs/run-label.sh` (full 13-model OpenRouter roster).
