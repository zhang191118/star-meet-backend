#!/usr/bin/env python3
"""Merge occupation / comment translations into production localized_profile JSON.

Inputs:
- the production dump of characters.localized_profile (authoritative for the
  existing keys),
- the hand-built occupation/comment translations (en + zh-Hans),
- the source data TSV (character_id, name, occupation, comment, tags_json) used
  as the zh-Hant original text.

For every public character the merged profile keeps all existing keys and adds
`occupation` and `comment` under en / zh-Hant / zh-Hans. The zh-Hant value is
the original legacy column text. Output is the JSON array shape consumed by
generate_localized_profile_sql.py.
"""

from __future__ import annotations

import argparse
import csv
import json
from pathlib import Path
from typing import Any


LANGS = ("en", "zh-Hant", "zh-Hans")
NEW_FIELDS = ("occupation", "comment")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--production", type=Path, default=Path("scripts/generated/localized_profiles_production_dump.json"))
    parser.add_argument("--translations", type=Path, default=Path("scripts/generated/occupation_comment_translations.json"))
    parser.add_argument("--source", type=Path, default=Path("scripts/generated/character_source_data.tsv"))
    parser.add_argument("--output", type=Path, default=Path("scripts/generated/localized_profiles_with_occupation_comment.json"))
    args = parser.parse_args()

    production = json.loads(args.production.read_text(encoding="utf-8"))
    translations = json.loads(args.translations.read_text(encoding="utf-8"))
    sources = {row["character_id"]: row for row in csv.DictReader(args.source.open(encoding="utf-8"), delimiter="\t")}

    errors: list[str] = []
    merged_rows: list[dict[str, Any]] = []
    for row in production:
        character_id = row["character_id"]
        profile = json.loads(row["localized_profile_raw"])

        source = sources.get(character_id)
        if source is None:
            errors.append(f"{character_id}: missing from source data")
            continue
        translation = translations.get(character_id)
        if translation is None:
            errors.append(f"{character_id}: missing translation")
            continue

        merged: dict[str, Any] = {}
        for lang in LANGS:
            base = profile.get(lang)
            if not isinstance(base, dict):
                errors.append(f"{character_id}: production profile lacks {lang}")
                continue
            item = dict(base)  # keep every existing key verbatim

            original = {
                "occupation": (source.get("occupation") or "").strip(),
                "comment": (source.get("comment") or "").strip(),
            }
            for field in NEW_FIELDS:
                if lang == "zh-Hant":
                    item[field] = original[field]
                    continue
                value = (translation.get(lang, {}).get(field) or "").strip()
                if not value and original[field]:
                    errors.append(f"{character_id}: empty {lang}.{field} translation")
                    continue
                item[field] = value
            merged[lang] = item

        if len(merged) == len(LANGS):
            merged_rows.append({"character_id": character_id, "localized_profile": merged})

    if errors:
        for error in errors:
            print("ERROR:", error)
        raise SystemExit(f"merge failed with {len(errors)} error(s)")

    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(merged_rows, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {len(merged_rows)} merged profiles -> {args.output}")


if __name__ == "__main__":
    main()
