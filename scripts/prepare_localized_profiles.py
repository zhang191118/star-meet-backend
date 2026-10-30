#!/usr/bin/env python3
"""Build the source manifest for en / zh-Hant / zh-Hans character profiles."""

from __future__ import annotations

import argparse
import json
import subprocess
from pathlib import Path
from typing import Any


LEGACY_NAMES = {
    "Leo": {"zh-Hans": "季夏", "zh-Hant": "季夏"},
    "Mia": {"zh-Hans": "小软", "zh-Hant": "小軟"},
    "Len": {"zh-Hans": "温言", "zh-Hant": "溫言"},
    "Rex": {"zh-Hans": "烈风", "zh-Hant": "烈風"},
    "Kazuki": {"zh-Hans": "寒川", "zh-Hant": "寒川"},
    "Arthur": {"zh-Hans": "沉渊", "zh-Hant": "沉淵"},
    "Asahi": {"zh-Hans": "旭阳", "zh-Hant": "旭陽"},
    "Mio": {"zh-Hans": "念念", "zh-Hant": "念念"},
    "Sunny": {"zh-Hans": "甜甜", "zh-Hant": "甜甜"},
    "Jiaojiao": {"zh-Hans": "娇娇", "zh-Hant": "嬌嬌"},
    "Iris": {"zh-Hans": "知知", "zh-Hant": "知知"},
    "Vivian": {"zh-Hans": "御薇", "zh-Hant": "御薇"},
    "Buddy": {"zh-Hans": "阿黄", "zh-Hant": "阿黃"},
    "Scarlet": {"zh-Hans": "红袖", "zh-Hant": "紅袖"},
    "Jiang": {"zh-Hans": "江寒", "zh-Hant": "江寒"},
}


def public_characters(path: Path) -> list[dict[str, Any]]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    data = payload.get("data", payload)
    characters = data.get("list", data) if isinstance(data, dict) else data
    if not isinstance(characters, list) or len(characters) != 95:
        raise SystemExit(f"expected 95 public characters in {path}, got {type(characters).__name__} {len(characters) if isinstance(characters, list) else ''}")
    return characters


def to_simplified(value: str) -> str:
    if not value:
        return value
    try:
        result = subprocess.run(
            ["/Applications/ServBay/bin/uconv", "-x", "Traditional-Simplified"],
            input=value,
            text=True,
            capture_output=True,
            check=True,
        )
        return result.stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        return value


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--public", type=Path, default=Path("/tmp/pub1.json"))
    parser.add_argument("--source-dir", type=Path, default=Path("/Users/guoliang/Documents/日常对话/crushie_data"))
    parser.add_argument("--output", type=Path, default=Path("scripts/generated/localized_profile_sources.json"))
    args = parser.parse_args()

    source_dir = args.source_dir
    crushie = json.loads((source_dir / "crushie_80_translated.json").read_text(encoding="utf-8"))
    by_character_id = {item["character_id"]: item for item in crushie}
    all_list = {
        item["secretCharacterId"]: item
        for item in json.loads((source_dir / "crushie_all_list.json").read_text(encoding="utf-8"))
    }
    english_records = {}
    for filename in ("top10_localized.json", "top70_named.json"):
        for item in json.loads((source_dir / filename).read_text(encoding="utf-8")):
            english_records[item["secretCharacterId"]] = item

    output = []
    for public in public_characters(args.public):
        character_id = public["character_id"]
        crushie_item = by_character_id.get(character_id)
        legacy = LEGACY_NAMES.get(public.get("name", ""))
        if crushie_item:
            english = english_records.get(crushie_item["source_character_id"], {})
            output.append({
                "character_id": character_id,
                "source_type": "crushie",
                "gender": public.get("gender", ""),
                "age": public.get("age", 22),
                "occupation": public.get("occupation", ""),
                "personality": public.get("personality", []),
                "relationship": public.get("relationship", ""),
                "name_zh_hant": crushie_item["name"],
                "name_zh_hans": to_simplified(crushie_item["name"]),
                "name_en_source": all_list.get(crushie_item["source_character_id"], {}).get("name") or english.get("name", crushie_item.get("source_name", "")),
                "english_source": {
                    "narrative": english.get("narrative", ""),
                    "basic_setting": english.get("basicSetting", ""),
                },
                "existing_zh_hant": {
                    "narrative": crushie_item.get("narrative", ""),
                    "basic_setting": crushie_item.get("basic_setting", ""),
                },
            })
        else:
            output.append({
                "character_id": character_id,
                "source_type": "legacy",
                "gender": public.get("gender", ""),
                "age": public.get("age", 22),
                "occupation": public.get("occupation", ""),
                "personality": public.get("personality", []),
                "relationship": public.get("relationship", ""),
                "name_zh_hant": (legacy or {}).get("zh-Hant", public.get("name", "")),
                "name_zh_hans": (legacy or {}).get("zh-Hans", public.get("name", "")),
                "name_en_source": public.get("name", ""),
                "english_source": {},
                "existing_zh_hant": {
                    "narrative": public.get("narrative", ""),
                    "basic_setting": public.get("basic_setting", ""),
                },
            })

    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(output, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {len(output)} profile sources -> {args.output}")
    print(f"crushie={sum(item['source_type'] == 'crushie' for item in output)} legacy={sum(item['source_type'] == 'legacy' for item in output)}")


if __name__ == "__main__":
    main()
