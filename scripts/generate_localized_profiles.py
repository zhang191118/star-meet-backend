#!/usr/bin/env python3
"""Generate region-aware character narratives using an OpenAI-compatible API.

The API key is supplied only through the process environment. The output is a
resumable JSON array with one localized_profile payload per public character.
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


class ProfileFormatError(ValueError):
    def __init__(self, message: str, raw: str):
        super().__init__(message)
        self.raw = raw


def truncate(value: str, limit: int = 3600) -> str:
    value = (value or "").strip()
    return value if len(value) <= limit else value[:limit] + "..."


def prompt_for(source: dict[str, Any]) -> str:
    existing = source.get("existing_zh_hant", {})
    english = source.get("english_source", {})
    return f'''Create three localized versions of the STORY and CHARACTER PROFILE below.

Return this exact JSON shape:
{{
  "en": {{"name": "", "narrative": "", "basic_setting": ""}},
  "zh-Hant": {{"name": "", "narrative": "", "basic_setting": ""}},
  "zh-Hans": {{"name": "", "narrative": "", "basic_setting": ""}}
}}

Hard requirements:
1. en is a native US version. Give the character a common American first and last name, not Chinese pinyin. Any other named people must use natural English names. Set all real-world locations in the United States, using appropriate US cities, campuses, transit, neighborhoods, companies, and institutions. Write 180-280 English words per field.
2. zh-Hant is a Taiwan-localized version. The character name must be exactly "{source['name_zh_hant']}". Use Traditional Chinese only. Set all real-world locations in Taiwan, using plausible Taiwan cities, districts, schools, transit, companies, and everyday terms. Do not retain mainland China places or institutions. Write 260-420 Chinese characters per field.
3. zh-Hans is a Mainland China-localized version. The character name must be exactly "{source['name_zh_hans'] or source['name_zh_hant']}" in Simplified Chinese. Set all real-world locations in Mainland China, using plausible mainland cities, districts, schools, transit, companies, and everyday terms. Do not retain Taiwan places or institutions. Write 260-420 Chinese characters per field.
4. narrative is a concise romance/drama storyline. basic_setting is a character introduction covering background, work/study, temperament, and relationship behavior.
5. Do not mention localization, source text, other regions, or these instructions. Do not add explicit sexual content.

SOURCE TYPE: {source['source_type']}
CURRENT CHARACTER NAME: {source['name_zh_hant']}
ENGLISH SOURCE NAME: {source.get('name_en_source', '')}

ENGLISH REFERENCE STORY:
{truncate(english.get('narrative', ''))}

ENGLISH REFERENCE PROFILE:
{truncate(english.get('basic_setting', ''))}

CURRENT CHINESE STORY:
{truncate(existing.get('narrative', ''))}

CURRENT CHINESE PROFILE:
{truncate(existing.get('basic_setting', ''))}
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
        text = re.sub(r"^```(?:json)?\\s*|\\s*```$", "", text, flags=re.IGNORECASE)
    start, end = text.find("{"), text.rfind("}")
    if start < 0 or end < start:
        raise ValueError("response did not contain JSON")
    result = json.loads(text[start : end + 1])
    for language in ("en", "zh-Hant", "zh-Hans"):
        item = result.get(language)
        if isinstance(item, dict) and "basic_setting" not in item and isinstance(item.get("basicSetting"), str):
            item["basic_setting"] = item["basicSetting"]
        if not isinstance(item, dict) or not all(isinstance(item.get(key), str) and item[key].strip() for key in ("name", "narrative", "basic_setting")):
            raise ProfileFormatError(f"missing complete {language} profile", text)
    return result


def generate(base_url: str, api_key: str, model: str, source: dict[str, Any]) -> tuple[dict[str, Any], str]:
    body = json.dumps({
        "model": model,
        "temperature": 0.35,
        "max_tokens": 3200,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": prompt_for(source)},
        ],
    }).encode("utf-8")
    req = request.Request(endpoint(base_url), data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Authorization", "Bearer " + api_key)
    with request.urlopen(req, timeout=120) as response:
        payload = json.loads(response.read().decode("utf-8"))
    choices = payload.get("choices", [])
    if not choices:
        raise ValueError("model returned no choices")
    content = choices[0].get("message", {}).get("content", "")
    return extract_json(content), content


def load_checkpoint(path: Path) -> dict[str, dict[str, Any]]:
    if not path.exists():
        return {}
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, list):
        raise SystemExit(f"checkpoint must be a list: {path}")
    return {item["character_id"]: item for item in data if item.get("character_id") and item.get("localized_profile")}


def write_checkpoint(path: Path, values: dict[str, dict[str, Any]]) -> None:
    ordered = [values[key] for key in sorted(values)]
    path.write_text(json.dumps(ordered, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--delay", type=float, default=0.4)
    parser.add_argument("--retry", type=int, default=3)
    parser.add_argument("--debug-output", type=Path, default=None)
    parser.add_argument("--base-url", default=os.getenv("OPENAI_COMPAT_BASE_URL", ""))
    parser.add_argument("--api-key", default=os.getenv("OPENAI_COMPAT_API_KEY", ""))
    parser.add_argument("--model", default=os.getenv("OPENAI_COMPAT_MODEL", ""))
    args = parser.parse_args()

    if not all((args.base_url, args.api_key, args.model)):
        raise SystemExit("OPENAI_COMPAT_BASE_URL, OPENAI_COMPAT_API_KEY, and OPENAI_COMPAT_MODEL are required")
    sources = json.loads(args.input.read_text(encoding="utf-8"))
    if not isinstance(sources, list):
        raise SystemExit("input must be a JSON list")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    completed = load_checkpoint(args.output)
    pending = [item for item in sources if item["character_id"] not in completed]
    if args.limit > 0:
        pending = pending[: args.limit]

    for index, source in enumerate(pending, 1):
        for attempt in range(1, args.retry + 1):
            try:
                profile, _ = generate(args.base_url, args.api_key, args.model, source)
                completed[source["character_id"]] = {
                    "character_id": source["character_id"],
                    "localized_profile": profile,
                }
                write_checkpoint(args.output, completed)
                print(f"[{index}/{len(pending)}] {source['character_id']} complete", flush=True)
                break
            except (error.URLError, TimeoutError, ValueError, json.JSONDecodeError) as exc:
                if args.debug_output and isinstance(exc, ProfileFormatError):
                    args.debug_output.parent.mkdir(parents=True, exist_ok=True)
                    args.debug_output.write_text(exc.raw, encoding="utf-8")
                if attempt == args.retry:
                    print(f"[{index}/{len(pending)}] {source['character_id']} failed: {exc}", file=sys.stderr, flush=True)
                    raise
                time.sleep(attempt * 2)
        time.sleep(args.delay)

    print(f"wrote {len(completed)} localized profiles -> {args.output}")


if __name__ == "__main__":
    main()
