#!/usr/bin/env python3
"""Build the source manifest for localizing character detail fields.

The detail fields (example / first_situation / first_words / background) are
stored as legacy Traditional-Chinese columns. This script fetches every public
character's detail from the API in the three market locales:

- zh-Hant (TW) responses carry the legacy columns themselves — the source text.
- en (US) / zh-Hans (CN) responses carry the already-localized
  name / narrative / basic_setting — used as consistency context so the new
  translations keep the same localized names and places.

Output: a resumable JSON array, one entry per public character.
"""

from __future__ import annotations

import argparse
import json
import time
from pathlib import Path
from typing import Any
from urllib import error, request


DETAIL_FIELDS = ("example", "first_situation", "first_words", "background")
CONTEXT_FIELDS = ("name", "narrative", "basic_setting")
LOCALES = {
    "zh-Hant": {"Accept-Language": "zh-Hant", "X-Client-Region": "TW"},
    "zh-Hans": {"Accept-Language": "zh-Hans", "X-Client-Region": "CN"},
    "en": {"Accept-Language": "en", "X-Client-Region": "US"},
}


def api_get(base_url: str, path: str, locale: str, retry: int = 3) -> Any:
    url = base_url.rstrip("/") + path
    req = request.Request(url)
    for header, value in LOCALES[locale].items():
        req.add_header(header, value)
    last: Exception | None = None
    for attempt in range(1, retry + 1):
        try:
            with request.urlopen(req, timeout=30) as resp:
                payload = json.loads(resp.read().decode("utf-8"))
            data = payload.get("data", payload)
            return data
        except (error.URLError, TimeoutError, json.JSONDecodeError) as exc:
            last = exc
            time.sleep(attempt * 1.5)
    raise SystemExit(f"GET {url} ({locale}) failed after {retry} attempts: {last}")


def public_character_ids(base_url: str) -> list[dict[str, Any]]:
    cards: list[dict[str, Any]] = []
    page = 1
    while True:
        data = api_get(base_url, f"/api/v1/character/public?page={page}&page_size=100", "zh-Hant")
        batch = data.get("list", []) if isinstance(data, dict) else data
        if not batch:
            break
        cards.extend(batch)
        if not data.get("has_more", False):
            break
        page += 1
    unique = {card["character_id"]: card for card in cards}
    return list(unique.values())


def load_checkpoint(path: Path) -> dict[str, dict[str, Any]]:
    if not path.exists():
        return {}
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, list):
        raise SystemExit(f"checkpoint must be a list: {path}")
    return {item["character_id"]: item for item in data if item.get("character_id")}


def write_checkpoint(path: Path, values: dict[str, dict[str, Any]]) -> None:
    ordered = [values[key] for key in sorted(values)]
    path.write_text(json.dumps(ordered, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="https://starmeet.gofastdating.com")
    parser.add_argument("--output", type=Path, default=Path("scripts/generated/localized_detail_sources.json"))
    parser.add_argument("--delay", type=float, default=0.15)
    args = parser.parse_args()

    cards = public_character_ids(args.base_url)
    print(f"found {len(cards)} public characters", flush=True)

    args.output.parent.mkdir(parents=True, exist_ok=True)
    completed = load_checkpoint(args.output)
    pending = [card for card in cards if card["character_id"] not in completed]

    for index, card in enumerate(pending, 1):
        character_id = card["character_id"]
        details: dict[str, Any] = {}
        for locale in LOCALES:
            detail = api_get(args.base_url, f"/api/v1/character/{character_id}", locale)
            details[locale] = detail
            time.sleep(args.delay)

        source = details["zh-Hant"]
        completed[character_id] = {
            "character_id": character_id,
            "gender": card.get("gender", ""),
            "age": card.get("age", 0),
            "occupation": card.get("occupation", ""),
            "source_zh_hant": {field: (source.get(field) or "").strip() for field in DETAIL_FIELDS},
            "locales": {
                locale: {field: (details[locale].get(field) or "").strip() for field in CONTEXT_FIELDS}
                for locale in LOCALES
            },
        }
        write_checkpoint(args.output, completed)
        missing = [field for field in DETAIL_FIELDS if not completed[character_id]["source_zh_hant"][field]]
        print(f"[{index}/{len(pending)}] {character_id} done" + (f" (missing: {', '.join(missing)})" if missing else ""), flush=True)

    print(f"wrote {len(completed)} detail sources -> {args.output}")


if __name__ == "__main__":
    main()
