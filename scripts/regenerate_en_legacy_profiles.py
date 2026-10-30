#!/usr/bin/env python3
"""Regenerate corrupted English narrative / basic_setting for legacy characters.

A previous localization round left several legacy characters with broken en
profiles (Chinese punctuation inside English text, sentences reduced to bare
punctuation). This script rebuilds the en narrative and basic_setting from the
intact zh-Hant / zh-Hans versions, keeping the en character name already stored
in production so references stay consistent.

The API key is supplied only through the process environment. The output is a
resumable JSON array: [{"character_id", "en": {"narrative", "basic_setting"}}].
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
from pathlib import Path
from typing import Any
from urllib import error, request


SYSTEM_PROMPT = """You are a senior fiction localization editor for a relationship-character app.
Return only valid JSON. Never include markdown, analysis, or code fences.
You preserve the character's emotional arc, occupation, age, and genre, but fully localize cultural context."""

BROKEN_PATTERN = re.compile(r"[，。、；]{3,}|[a-zA-Z][，。]{2,}")
CJK_OR_PUNCT = re.compile(r"[\u4e00-\u9fff，。、；：！？「」（）]")


class ProfileFormatError(ValueError):
    def __init__(self, message: str, raw: str):
        super().__init__(message)
        self.raw = raw


def find_broken_ids(rows: list[dict[str, Any]]) -> set[str]:
    broken: set[str] = set()
    for row in rows:
        profile = json.loads(row["localized_profile_raw"])
        en = profile.get("en", {})
        for field in ("narrative", "basic_setting"):
            if BROKEN_PATTERN.search(en.get(field, "")):
                broken.add(row["character_id"])
    return broken


def prompt_for(profile: dict[str, Any]) -> str:
    en_name = profile["en"]["name"]
    return f'''Rewrite the English STORY and CHARACTER PROFILE for this character.

Return this exact JSON shape:
{{"en": {{"narrative": "", "basic_setting": ""}}}}

Hard requirements:
1. The character name must be exactly "{en_name}". Other people must have natural English names.
2. Set all real-world locations in the United States, using plausible US cities, campuses, transit, neighborhoods, companies, and institutions. Match the city already used in the English reference when one is given.
3. Write 180-280 English words per field.
4. narrative is a concise romance/drama storyline in second person ("you"). basic_setting is a character introduction covering background, work/study, temperament, and relationship behavior.
5. Preserve the emotional arc and all character facts from the Chinese versions below.
6. Output must be 100% English — no Chinese characters or Chinese punctuation. Do not mention localization, source text, or these instructions. Do not add explicit sexual content.

TRADITIONAL CHINESE STORY:
{profile["zh-Hant"]["narrative"]}

TRADITIONAL CHINESE PROFILE:
{profile["zh-Hant"]["basic_setting"]}

SIMPLIFIED CHINESE STORY (reference for market-adapted details):
{profile["zh-Hans"]["narrative"]}

SIMPLIFIED CHINESE PROFILE:
{profile["zh-Hans"]["basic_setting"]}
'''


def endpoint(base_url: str) -> str:
    base_url = base_url.rstrip("/")
    if base_url.endswith("/v1/chat/completions") or base_url.endswith("/chat/completions"):
        return base_url
    if base_url.endswith("/v1"):
        return base_url + "/chat/completions"
    return base_url + "/v1/chat/completions"


def extract_json(text: str) -> dict[str, Any]:
    text = text.strip()
    if text.startswith("```"):
        text = re.sub(r"^```(?:json)?\s*|\s*```$", "", text, flags=re.IGNORECASE)
    start, end = text.find("{"), text.rfind("}")
    if start < 0 or end < start:
        raise ValueError("response did not contain JSON")
    result = json.loads(text[start : end + 1], strict=False)
    en = result.get("en", result)
    for field in ("narrative", "basic_setting"):
        value = en.get(field, "") if isinstance(en, dict) else ""
        if not isinstance(value, str) or len(value.split()) < 120:
            raise ProfileFormatError(f"en.{field} missing or too short", text)
        if CJK_OR_PUNCT.search(value):
            raise ProfileFormatError(f"en.{field} contains Chinese characters or punctuation", text)
    return {"narrative": en["narrative"].strip(), "basic_setting": en["basic_setting"].strip()}


def generate(base_url: str, api_key: str, model: str, profile: dict[str, Any]) -> dict[str, Any]:
    body = json.dumps({
        "model": model,
        "temperature": 0.35,
        "max_tokens": 2500,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": prompt_for(profile)},
        ],
    }).encode("utf-8")
    req = request.Request(endpoint(base_url), data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Authorization", "Bearer " + api_key)
    with request.urlopen(req, timeout=180) as response:
        payload = json.loads(response.read().decode("utf-8"))
    choices = payload.get("choices", [])
    if not choices:
        raise ValueError("model returned no choices")
    return extract_json(choices[0].get("message", {}).get("content", ""))


def load_checkpoint(path: Path) -> dict[str, dict[str, Any]]:
    if not path.exists():
        return {}
    data = json.loads(path.read_text(encoding="utf-8"))
    return {item["character_id"]: item for item in data if item.get("character_id") and item.get("en")}


def write_checkpoint(path: Path, values: dict[str, dict[str, Any]]) -> None:
    ordered = [values[key] for key in sorted(values)]
    path.write_text(json.dumps(ordered, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--production", type=Path, default=Path("scripts/generated/localized_profiles_production_dump.json"))
    parser.add_argument("--output", type=Path, default=Path("scripts/generated/en_legacy_profiles.json"))
    parser.add_argument("--retry", type=int, default=5)
    parser.add_argument("--delay", type=float, default=0.4)
    parser.add_argument("--debug-output", type=Path, default=None)
    parser.add_argument("--base-url", default=os.getenv("OPENAI_COMPAT_BASE_URL", ""))
    parser.add_argument("--api-key", default=os.getenv("OPENAI_COMPAT_API_KEY", ""))
    parser.add_argument("--model", default=os.getenv("OPENAI_COMPAT_MODEL", ""))
    args = parser.parse_args()

    if not all((args.base_url, args.api_key, args.model)):
        raise SystemExit("OPENAI_COMPAT_BASE_URL, OPENAI_COMPAT_API_KEY, and OPENAI_COMPAT_MODEL are required")
    rows = json.loads(args.production.read_text(encoding="utf-8"))
    broken_ids = find_broken_ids(rows)
    profiles = {row["character_id"]: json.loads(row["localized_profile_raw"]) for row in rows if row["character_id"] in broken_ids}
    print(f"broken en profiles: {len(broken_ids)}", flush=True)

    args.output.parent.mkdir(parents=True, exist_ok=True)
    completed = load_checkpoint(args.output)
    pending = [cid for cid in sorted(broken_ids) if cid not in completed]

    for index, character_id in enumerate(pending, 1):
        for attempt in range(1, args.retry + 1):
            try:
                en = generate(args.base_url, args.api_key, args.model, profiles[character_id])
                completed[character_id] = {"character_id": character_id, "en": en}
                write_checkpoint(args.output, completed)
                print(f"[{index}/{len(pending)}] {character_id} complete", flush=True)
                break
            except (error.URLError, TimeoutError, OSError, ValueError, json.JSONDecodeError) as exc:
                if args.debug_output and isinstance(exc, ProfileFormatError):
                    args.debug_output.parent.mkdir(parents=True, exist_ok=True)
                    args.debug_output.write_text(exc.raw, encoding="utf-8")
                if attempt == args.retry:
                    raise RuntimeError(f"{character_id} failed: {exc}") from exc
                time.sleep(3 * attempt ** 2 if isinstance(exc, (error.URLError, TimeoutError, OSError)) else attempt * 2)
        time.sleep(args.delay)

    print(f"wrote {len(completed)} rebuilt en profiles -> {args.output}")


if __name__ == "__main__":
    main()
