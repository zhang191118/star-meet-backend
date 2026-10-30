#!/usr/bin/env python3
"""Normalize the 80 localized Crushie characters into an import-ready JSON.

This script only prepares local artifacts. It does not upload files or mutate a
database. The import/deploy step consumes the generated JSON and performs its
own backup and verification.
"""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path
from typing import Any


FIELDS = (
    "comment",
    "narrative",
    "basicSetting",
    "personality",
    "example",
    "firstSituation",
    "firstWords",
)

ROLE_TAGS = {
    "音樂人", "學生", "運動員", "專業", "專業精英", "總裁", "億萬富翁", "牛仔",
    "保鏢", "警察", "藝術家", "虛擬主播", "執行長", "教師", "偵探", "作家",
    "大學生", "教授", "醫生", "工程師", "船長", "黑手黨", "保護", "專業，高冷，體貼",
}
STORY_TAGS = {
    "浪漫", "暗戀", "相愛相殺", "冒險", "奇幻", "幻想", "復仇", "重生", "犯罪",
    "秘密", "初戀", "失憶", "保護", "OC", "原創角",
}
ORIGIN_TAGS = {"原創", "原創角色", "原創角"}


def parse_age(value: Any) -> int:
    match = re.search(r"\d+", str(value or ""))
    return int(match.group(0)) if match else 25


def grouped_tags(tags: Any) -> dict[str, list[str]]:
    if isinstance(tags, str):
        tags = [part.strip() for part in tags.split(",") if part.strip()]
    if not isinstance(tags, list):
        tags = []

    result = {"Roles": [], "Personality": [], "Story": [], "Origin": []}
    for raw in tags:
        tag = str(raw).strip()
        if not tag:
            continue
        if tag in ORIGIN_TAGS:
            group = "Origin"
        elif tag in STORY_TAGS:
            group = "Story"
        elif tag in ROLE_TAGS:
            group = "Roles"
        else:
            group = "Personality"
        if tag not in result[group]:
            result[group].append(tag)
    if not result["Origin"]:
        result["Origin"] = ["原創"]
    return result


def system_prompt(character: dict[str, Any]) -> str:
    name = character["name"]
    age = character["age"]
    occupation = character["occupation"]
    return (
        f"你是{name}，{age}歲，{occupation}。\n"
        f"{character['comment']}\n\n"
        f"【人設】\n{character['basic_setting']}\n\n"
        f"【故事背景】\n{character['narrative']}\n\n"
        f"【性格】\n{character['personality_text']}\n\n"
        f"【說話風格】\n{character['example']}\n\n"
        f"【開場場景】\n{character['first_situation']}\n\n"
        f"【你的第一句話】\n{character['first_words']}\n\n"
        f"請用繁體中文回覆，完全代入{name}的角色，用(*)描述動作，嚴格遵守說話風格規範。"
    )


def load_rows(source_dir: Path) -> list[dict[str, str]]:
    rows = []
    for line in (source_dir / "db_rows.tsv").read_text(encoding="utf-8").splitlines():
        db_id, character_id, name, age, occupation = line.split("\t", 4)
        rows.append({"db_id": db_id, "character_id": character_id, "name": name, "age": age, "occupation": occupation})
    return rows


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-dir", type=Path, default=Path("/Users/guoliang/Documents/日常对话/crushie_data"))
    parser.add_argument("--top70", type=Path, default=None)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()

    source_dir = args.source_dir
    output = args.output or (source_dir / "crushie_80_translated.json")
    prompt_pack = {item["secretCharacterId"]: item for item in json.loads((source_dir / "crushie_official_prompt_pack.json").read_text(encoding="utf-8"))}
    top10 = json.loads((source_dir / "top10_translated.json").read_text(encoding="utf-8"))
    top70_path = args.top70 or (source_dir / "top70_translated.json")
    top70 = json.loads(top70_path.read_text(encoding="utf-8"))
    translated = {item["secretCharacterId"]: item for item in top10 + top70}
    rows = load_rows(source_dir)

    if len(rows) != 80 or len(translated) != 80:
        raise SystemExit(f"expected 80 rows/translations, got rows={len(rows)} translations={len(translated)}")

    result = []
    for row in rows:
        item = translated.get(next((sid for sid, value in translated.items() if value.get("character_id") == row["character_id"]), ""))
        if item is None:
            item = next((value for value in translated.values() if value.get("name") == row["name"] or value.get("tw_name") == row["name"]), None)
        if item is None:
            raise SystemExit(f"translation not found for {row['character_id']} / {row['name']}")

        name = item.get("tw_name") or item.get("name")
        source = prompt_pack.get(item["secretCharacterId"], {})
        tags = grouped_tags(item.get("tags", []))
        personality_tags = [tag for group in tags.values() for tag in group]
        character = {
            "character_id": row["character_id"],
            "source_character_id": item["secretCharacterId"],
            "source_name": source.get("name") or item.get("name", ""),
            "name": name,
            "gender": (item.get("gender") or {}).get("value", "male") if isinstance(item.get("gender"), dict) else (item.get("gender") or "male"),
            "age": parse_age(item.get("age") or row["age"]),
            "occupation": item.get("job") or row["occupation"],
            "background": item.get("basicSetting", ""),
            "personality": json.dumps(tags["Personality"], ensure_ascii=False),
            "personality_text": item.get("personality", ""),
            "personality_tags": json.dumps(personality_tags, ensure_ascii=False),
            "relationship": "伙伴",
            "style": "冷淡型" if any(tag in tags["Personality"] for tag in ("高冷", "冷酷", "冷漠", "強勢霸道")) else "温暖型",
            "avatar": f"https://star-meet.oss-cn-hangzhou.aliyuncs.com/characters/crushie/{row['character_id']}_avatar.png",
            "local_avatar": str(source_dir / "avatars" / f"{row['character_id']}.png"),
            "source_picture": item.get("picture", ""),
            "comment": item.get("comment", ""),
            "narrative": item.get("narrative", ""),
            "basic_setting": item.get("basicSetting", ""),
            "example": item.get("example", ""),
            "first_situation": item.get("firstSituation", ""),
            "first_words": item.get("firstWords", ""),
            "tags_json": json.dumps(tags, ensure_ascii=False),
        }
        character["system_prompt"] = system_prompt(character)
        result.append(character)

    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(result, ensure_ascii=False, indent=2) + chr(10), encoding="utf-8")
    print(f"wrote {len(result)} characters -> {output}")


if __name__ == "__main__":
    main()
