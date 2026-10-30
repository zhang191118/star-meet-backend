#!/usr/bin/env python3
"""Merge generated detail translations into the production localized_profile JSON.

Inputs:
- the production dump of characters.localized_profile (authoritative for the
  existing name / narrative / basic_setting keys),
- the generated detail translations (example / first_situation / first_words /
  background per locale),
- the source manifest (to know which legacy fields were non-empty and must
  therefore have a translation).

Output: the same shape generate_localized_profile_sql.py consumes — a JSON
array of {"character_id", "localized_profile"} rows with the merged profiles.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


DETAIL_FIELDS = ("example", "first_situation", "first_words", "background")
CORE_FIELDS = ("name", "narrative", "basic_setting")
LANGS = ("en", "zh-Hant", "zh-Hans")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--production", type=Path, default=Path("scripts/generated/localized_profiles_production_dump.json"))
    parser.add_argument("--details", type=Path, default=Path("scripts/generated/localized_details.json"))
    parser.add_argument("--sources", type=Path, default=Path("scripts/generated/localized_detail_sources.json"))
    parser.add_argument("--output", type=Path, default=Path("scripts/generated/localized_profiles_with_details.json"))
    args = parser.parse_args()

    production = json.loads(args.production.read_text(encoding="utf-8"))
    details = {item["character_id"]: item["details"] for item in json.loads(args.details.read_text(encoding="utf-8"))}
    sources = {item["character_id"]: item for item in json.loads(args.sources.read_text(encoding="utf-8"))}

    errors: list[str] = []
    merged_rows: list[dict[str, Any]] = []
    for row in production:
        character_id = row["character_id"]
        profile = json.loads(row["localized_profile_raw"])

        source = sources.get(character_id)
        if source is None:
            errors.append(f"{character_id}: missing from source manifest")
            continue
        generated = details.get(character_id)
        if generated is None:
            errors.append(f"{character_id}: missing generated details")
            continue

        expected_fields = [field for field in DETAIL_FIELDS if source["source_zh_hant"].get(field)]
        merged: dict[str, Any] = {}
        for lang in LANGS:
            base = profile.get(lang)
            if not isinstance(base, dict):
                errors.append(f"{character_id}: production profile lacks {lang}")
                continue
            item = {field: base.get(field, "") for field in CORE_FIELDS}
            for field in expected_fields:
                value = (generated.get(lang, {}).get(field) or "").strip()
                if not value:
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
