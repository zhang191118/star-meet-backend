#!/usr/bin/env python3
"""Generate localized detail fields using an OpenAI-compatible API.

Translates the legacy Traditional-Chinese detail columns (example,
first_situation, first_words, background) into the three market locales,
keeping each translation consistent with the character's already-localized
name / narrative / basic_setting for that locale.

The API key is supplied only through the process environment. The output is a
resumable JSON array with one details payload per public character.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Any
from urllib import error, request


DETAIL_FIELDS = ("example", "first_situation", "first_words", "background")

FIELD_NOTES = {
    "example": "example is a speaking-style guide: tone, pet names, humor, pacing rules. Keep the same rule count, structure, and format as the source.",
    "first_situation": "first_situation is the opening scene written in second person (the reader is 'you'). Keep the same scenario, emotional arc, and approximate length.",
    "first_words": "first_words is the character's very first line of dialogue, including its request or tension. Keep the same voice, intent, and approximate length.",
    "background": "background is the story's setting introduction. Keep the same facts and approximate length.",
}

SYSTEM_PROMPT = """You are a senior fiction localization editor for a relationship-character app.
Return only valid JSON. Never include markdown, analysis, or code fences.
You preserve the character's voice, emotional arc, and genre, but fully localize cultural context.
The en fields must be written entirely in American English — never leave any Chinese characters in them.
The zh-Hans fields must be set in Mainland China — never mention Taiwan, Taipei, Taichung, Kaohsiung, or other Taiwan places."""

TAIWAN_MARKERS = (
    "臺灣", "台湾", "台北", "臺北", "台中", "臺中", "高雄", "臺南", "台南",
    "新竹", "嘉義", "嘉義", "基隆", "宜蘭", "花蓮", "臺東", "台東", "苗栗",
    "彰化", "南投", "屏東", "澎湖",
)


class DetailsFormatError(ValueError):
    def __init__(self, message: str, raw: str):
        super().__init__(message)
        self.raw = raw


def truncate(value: str, limit: int = 3600) -> str:
    value = (value or "").strip()
    return value if len(value) <= limit else value[:limit] + "..."


def prompt_for(source: dict[str, Any]) -> str:
    fields = [field for field in DETAIL_FIELDS if source["source_zh_hant"].get(field)]
    locales = source["locales"]

    shape_rows = ",\n".join(
        f'  "{lang}": {{' + ", ".join(f'"{field}": ""' for field in fields) + "}"
        for lang in ("en", "zh-Hant", "zh-Hans")
    )
    field_notes = "\n".join(f"- {FIELD_NOTES[field]}" for field in fields)
    source_block = "\n".join(
        f"{field.upper()}:\n{truncate(source['source_zh_hant'][field])}" for field in fields
    )
    locale_blocks = []
    for lang, label in (("en", "US ENGLISH"), ("zh-Hant", "TRADITIONAL CHINESE (TAIWAN)"), ("zh-Hans", "SIMPLIFIED CHINESE (MAINLAND)")):
        profile = locales[lang]
        locale_blocks.append(f'''{label} version — character name must be exactly "{profile['name']}":
NARRATIVE:
{truncate(profile.get('narrative', ''))}
PROFILE:
{truncate(profile.get('basic_setting', ''))}''')

    return f'''Localize the CHARACTER DETAIL FIELDS below into three market versions.

Return this exact JSON shape:
{{
{shape_rows}
}}

Field semantics:
{field_notes}

Hard requirements:
1. Each language version must stay consistent with that language's existing localized NARRATIVE and PROFILE shown below: reuse the exact same character name, other people's names, places, schools, companies, and institutions. Never mix locations or names across languages.
2. en uses natural American English and US cultural context. zh-Hant uses Traditional Chinese with Taiwan usage only. zh-Hans uses Simplified Chinese with Mainland China usage only.
3. Preserve meaning, tone, and emotional beats of the source fields. Do not add new plot points and do not drop any rule or sentence from the source.
4. Keep each field roughly the same length as its source text.
5. Do not mention localization, source text, other regions, or these instructions. Do not add explicit sexual content.

CHARACTER: {locales['zh-Hant'].get('name', '')} ({source.get('gender', '')}, {source.get('age', '')}), occupation: {source.get('occupation', '')}

SOURCE DETAIL FIELDS (Traditional Chinese, the content to localize):
{source_block}

EXISTING LOCALIZED VERSIONS (context for names and places):

{chr(10).join(locale_blocks)}
'''


def endpoint(base_url: str) -> str:
    base_url = base_url.rstrip("/")
    if base_url.endswith("/v1/chat/completions") or base_url.endswith("/chat/completions"):
        return base_url
    if base_url.endswith("/v1"):
        return base_url + "/chat/completions"
    return base_url + "/v1/chat/completions"


# 键名归一化：模型偶尔输出 camelCase 或拼错（如 first_sitation），
# 按去掉非字母数字后的小写形式映射回规范键。
KEY_ALIASES = {
    "example": "example",
    "firstsituation": "first_situation",
    "firstsitation": "first_situation",
    "firstwords": "first_words",
    "firstword": "first_words",
    "basicsetting": "basic_setting",
    "background": "background",
}


def extract_json(text: str, fields: list[str]) -> dict[str, Any]:
    text = text.strip()
    if text.startswith("```"):
        text = re.sub(r"^```(?:json)?\s*|\s*```$", "", text, flags=re.IGNORECASE)
    start, end = text.find("{"), text.rfind("}")
    if start < 0 or end < start:
        raise ValueError("response did not contain JSON")
    # strict=False 容忍模型在字符串里输出的未转义换行等控制字符。
    result = json.loads(text[start : end + 1], strict=False)
    for item in result.values():
        if isinstance(item, dict):
            for key in list(item.keys()):
                normalized = re.sub(r"[^a-z0-9]", "", key.lower())
                canonical = KEY_ALIASES.get(normalized)
                if canonical and canonical != key and canonical not in item:
                    item[canonical] = item.pop(key)
    for language in ("en", "zh-Hant", "zh-Hans"):
        item = result.get(language)
        if not isinstance(item, dict) or not all(isinstance(item.get(field), str) and item[field].strip() for field in fields):
            raise DetailsFormatError(f"missing complete {language} details", text)
    for field in fields:
        if re.search(r"[\u4e00-\u9fff]", result["en"][field]):
            raise DetailsFormatError(f"en.{field} contains Chinese characters", text)
        for marker in TAIWAN_MARKERS:
            if marker in result["zh-Hans"][field]:
                raise DetailsFormatError(f"zh-Hans.{field} mentions Taiwan place {marker}", text)
    return result


def generate(base_url: str, api_key: str, model: str, source: dict[str, Any]) -> tuple[dict[str, Any], str]:
    fields = [field for field in DETAIL_FIELDS if source["source_zh_hant"].get(field)]
    body = json.dumps({
        "model": model,
        "temperature": 0.35,
        "max_tokens": 4096,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": prompt_for(source)},
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
    content = choices[0].get("message", {}).get("content", "")
    return extract_json(content, fields), content


def load_checkpoint(path: Path) -> dict[str, dict[str, Any]]:
    if not path.exists():
        return {}
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, list):
        raise SystemExit(f"checkpoint must be a list: {path}")
    return {item["character_id"]: item for item in data if item.get("character_id") and item.get("details")}


def write_checkpoint(path: Path, values: dict[str, dict[str, Any]]) -> None:
    ordered = [values[key] for key in sorted(values)]
    path.write_text(json.dumps(ordered, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", type=Path, default=Path("scripts/generated/localized_detail_sources.json"))
    parser.add_argument("--output", type=Path, default=Path("scripts/generated/localized_details.json"))
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--delay", type=float, default=0.4)
    parser.add_argument("--workers", type=int, default=4)
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

    lock = threading.Lock()
    counter = {"done": 0}
    total = len(pending)

    def process(source: dict[str, Any]) -> None:
        fields = [field for field in DETAIL_FIELDS if source["source_zh_hant"].get(field)]
        with lock:
            counter["done"] += 1
            index = counter["done"]
        if not fields:
            print(f"[{index}/{total}] {source['character_id']} skipped: no detail fields", flush=True)
            return
        for attempt in range(1, args.retry + 1):
            try:
                details, _ = generate(args.base_url, args.api_key, args.model, source)
                with lock:
                    completed[source["character_id"]] = {
                        "character_id": source["character_id"],
                        "details": details,
                    }
                    write_checkpoint(args.output, completed)
                print(f"[{index}/{total}] {source['character_id']} complete", flush=True)
                time.sleep(args.delay)
                return
            except (error.URLError, TimeoutError, OSError, ValueError, json.JSONDecodeError) as exc:
                if args.debug_output and isinstance(exc, DetailsFormatError):
                    with lock:
                        args.debug_output.parent.mkdir(parents=True, exist_ok=True)
                        args.debug_output.write_text(exc.raw, encoding="utf-8")
                if attempt == args.retry:
                    raise RuntimeError(f"{source['character_id']} failed: {exc}") from exc
                # 网络/DNS 抖动需要更长的退避，格式错误则快速重试即可。
                backoff = 3 * attempt ** 2 if isinstance(exc, (error.URLError, TimeoutError, OSError)) else attempt * 2
                time.sleep(backoff)

    failures: list[str] = []
    with ThreadPoolExecutor(max_workers=max(1, args.workers)) as pool:
        futures = [pool.submit(process, source) for source in pending]
        for future in as_completed(futures):
            try:
                future.result()
            except RuntimeError as exc:
                failures.append(str(exc))
                print("ERROR:", exc, file=sys.stderr, flush=True)

    if failures:
        raise SystemExit(f"{len(failures)} character(s) failed; rerun the script to retry the remaining ones")
    print(f"wrote {len(completed)} localized details -> {args.output}")


if __name__ == "__main__":
    main()
