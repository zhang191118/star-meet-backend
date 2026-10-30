#!/usr/bin/env python3
"""Normalize public character tags into the Crushie-style discover taxonomy.

The script is deliberately backup-first and does not connect to a database. It
reads the last public API payload, joins the 80 translated Crushie records with
their original English tags, preserves legacy tags, and emits an UPDATE-only
SQL file for review/application on the server.
"""

from __future__ import annotations

import argparse
import json
import shutil
from datetime import datetime
from pathlib import Path
from typing import Any


DISCOVER_IDS = ("保护欲", "高冷", "体贴", "强势", "脆弱", "霸总", "运动员", "非人")

ENGLISH_GROUPS: dict[str, tuple[str, str]] = {
    "Protective": ("Personality", "保护欲"),
    "Cold": ("Personality", "高冷"),
    "Caring": ("Personality", "体贴"),
    "Dominant": ("Personality", "强势"),
    "Vulnerable": ("Personality", "脆弱"),
    "Billionaire": ("Roles", "霸总"),
    "CEO": ("Roles", "霸总"),
    "Athlete": ("Roles", "运动员"),
    "NonHuman": ("Personality", "非人"),
    "Professional": ("Roles", "专业"),
    "Student": ("Roles", "学生"),
    "Musician": ("Roles", "音乐人"),
    "Cowboy": ("Roles", "牛仔"),
    "Bodyguard": ("Roles", "保镖"),
    "Police": ("Roles", "警察"),
    "Artist": ("Roles", "艺术家"),
    "Vtuber": ("Roles", "虚拟主播"),
    "Muscle": ("Roles", "肌肉"),
    "Sensitive": ("Personality", "敏感"),
    "Confident": ("Personality", "自信"),
    "Creative": ("Personality", "有创造力"),
    "Possessive": ("Personality", "占有欲"),
    "Possesive": ("Personality", "占有欲"),
    "Energetic": ("Personality", "活力"),
    "Charismatic": ("Personality", "魅力"),
    "Loyal": ("Personality", "忠诚"),
    "Insecure": ("Personality", "脆弱"),
    "Fantasy": ("Story", "奇幻"),
    "Romance": ("Story", "浪漫"),
    "Secret Crush": ("Story", "暗恋"),
    "Adventure": ("Story", "冒险"),
    "Crime": ("Story", "犯罪"),
    "Enemies to Lovers": ("Story", "相爱相杀"),
    "Revenge": ("Story", "复仇"),
    "Reborn": ("Story", "重生"),
    "OC": ("Origin", "原创"),
}

TAG_NORMALIZATION = {
    "保护欲强": "保护欲",
    "保護慾強": "保护欲",
    "保護欲強": "保护欲",
    "保護欲": "保护欲",
    "保護": "保护欲",
    "高冷独立": "高冷",
    "高冷獨立": "高冷",
    "冷淡疏离": "高冷",
    "冷淡疏離": "高冷",
    "冷酷": "高冷",
    "冷漠": "高冷",
    "清冷话少": "高冷",
    "清冷話少": "高冷",
    "体贴温柔": "体贴",
    "温柔体贴": "体贴",
    "體貼溫柔": "体贴",
    "溫柔體貼": "体贴",
    "體貼": "体贴",
    "關懷": "体贴",
    "强势霸道": "强势",
    "強勢霸道": "强势",
    "強勢": "强势",
    "霸道": "强势",
    "支配": "强势",
    "脆弱敏感": "脆弱",
    "沒安全感": "脆弱",
    "缺乏安全感": "脆弱",
    "敏感細膩": "敏感",
    "敏感细腻": "敏感",
    "佔有慾強": "占有欲",
    "占有欲强": "占有欲",
    "億萬富翁": "霸总",
    "亿万富翁": "霸总",
    "總裁": "霸总",
    "总裁": "霸总",
    "公司总裁": "霸总",
    "企業掌舵者": "霸总",
    "篮球爱好者": "运动员",
    "運動員": "运动员",
    "非人類": "非人",
    "非人类": "非人",
    "奇幻": "奇幻",
    "幻想": "奇幻",
    "原創": "原创",
    "原創角色": "原创",
    "原創角": "原创",
    "音樂人": "音乐人",
    "學生": "学生",
    "專業": "专业",
    "專業精英": "专业",
    "牛仔": "牛仔",
    "保鏢": "保镖",
    "警察": "警察",
    "藝術家": "艺术家",
    "虛擬主播": "虚拟主播",
    "肌肉": "肌肉",
    "自信": "自信",
    "有创造力": "有创造力",
    "有創造力": "有创造力",
    "創意": "有创造力",
    "活力充沛": "活力",
    "活力": "活力",
    "魅力十足": "魅力",
    "魅力": "魅力",
    "忠诚": "忠诚",
    "忠誠": "忠诚",
    "浪漫": "浪漫",
    "暗戀": "暗恋",
    "冒險": "冒险",
    "犯罪": "犯罪",
    "相愛相殺": "相爱相杀",
    "復仇": "复仇",
    "重生": "重生",
    "OC": "原创",
    "原創角色": "原创",
}

GROUPS = ("Roles", "Personality", "Story", "Origin")


def sql_string(value: Any) -> str:
    text = str(value or "").replace("\\", "\\\\").replace("'", "''")
    text = text.replace("\r", "\\r").replace("\n", "\\n")
    return "'" + text + "'"


def as_list(value: Any) -> list[str]:
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except json.JSONDecodeError:
            value = [part.strip() for part in value.split(",")]
    if isinstance(value, dict):
        values: list[str] = []
        for item in value.values():
            values.extend(as_list(item))
        return values
    if not isinstance(value, list):
        return []
    return [str(item).strip() for item in value if str(item).strip()]


def normalize_tag(tag: str) -> str:
    return TAG_NORMALIZATION.get(tag, tag)


def empty_groups() -> dict[str, list[str]]:
    return {group: [] for group in GROUPS}


def add(grouped: dict[str, list[str]], group: str, tag: str) -> None:
    if tag and tag not in grouped[group]:
        grouped[group].append(tag)


def normalize_groups(raw: Any) -> dict[str, list[str]]:
    grouped = empty_groups()
    if isinstance(raw, str):
        try:
            raw = json.loads(raw)
        except json.JSONDecodeError:
            raw = {}
    if isinstance(raw, dict):
        for group in GROUPS:
            for tag in as_list(raw.get(group, [])):
                add(grouped, group, normalize_tag(tag))
    return grouped


def apply_english_tags(grouped: dict[str, list[str]], tags: Any) -> None:
    for raw in as_list(tags):
        mapping = ENGLISH_GROUPS.get(raw)
        if mapping:
            add(grouped, mapping[0], mapping[1])


def apply_implicit_tags(grouped: dict[str, list[str]]) -> None:
    all_tags = {tag for values in grouped.values() for tag in values}
    if all_tags.intersection({"狗狗", "宠物", "陪伴宠物", "猫咪", "修勾"}):
        add(grouped, "Personality", "非人")
    if "奇幻" in all_tags or "幻想" in all_tags:
        add(grouped, "Personality", "非人")
    if "总裁" in all_tags or "公司总裁" in all_tags or "亿万富翁" in all_tags:
        add(grouped, "Roles", "霸总")
    if not grouped["Origin"]:
        grouped["Origin"] = ["原创"]


def compact_groups(grouped: dict[str, list[str]]) -> dict[str, list[str]]:
    for group in GROUPS:
        grouped[group] = list(dict.fromkeys(grouped[group]))
    return grouped


def load_public_items(path: Path) -> list[dict[str, Any]]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    data = payload.get("data", payload) if isinstance(payload, dict) else payload
    items = data.get("list", []) if isinstance(data, dict) else data
    if not isinstance(items, list):
        raise SystemExit(f"public payload has no list: {path}")
    return items


def build_updates(public_items: list[dict[str, Any]], crushie_path: Path, prompt_path: Path) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    crushie = json.loads(crushie_path.read_text(encoding="utf-8"))
    prompt = {item["secretCharacterId"]: item for item in json.loads(prompt_path.read_text(encoding="utf-8"))}
    by_character_id = {item["character_id"]: item for item in crushie}
    updates: list[dict[str, Any]] = []
    normalized_crushie: list[dict[str, Any]] = []

    for item in public_items:
        grouped = normalize_groups(item.get("tags_json", {}))
        crushie_item = by_character_id.get(item.get("character_id"))
        if crushie_item:
            grouped = normalize_groups(crushie_item.get("tags_json", grouped))
            source = prompt.get(crushie_item.get("source_character_id"), {})
            apply_english_tags(grouped, source.get("tags", []))
        else:
            apply_english_tags(grouped, item.get("tags_json", {}).get("tags", []) if isinstance(item.get("tags_json"), dict) else [])
        apply_implicit_tags(grouped)
        compact_groups(grouped)
        personality = grouped["Personality"]
        personality_tags = [tag for group in GROUPS for tag in grouped[group]]
        update = {
            "character_id": item["character_id"],
            "tags_json": json.dumps(grouped, ensure_ascii=False, separators=(",", ":")),
            "personality": json.dumps(personality, ensure_ascii=False, separators=(",", ":")),
            "personality_tags": json.dumps(personality_tags, ensure_ascii=False, separators=(",", ":")),
        }
        updates.append(update)
        if crushie_item:
            crushie_item["tags_json"] = json.dumps(grouped, ensure_ascii=False)
            crushie_item["personality"] = json.dumps(personality, ensure_ascii=False)
            crushie_item["personality_tags"] = json.dumps(personality_tags, ensure_ascii=False)
            normalized_crushie.append(crushie_item)
    return updates, normalized_crushie


def generate_sql(updates: list[dict[str, Any]], backup_table: str) -> str:
    ids = ",".join(sql_string(item["character_id"]) for item in updates)
    lines = [
        "SET NAMES utf8mb4;",
        "SET SESSION time_zone = '+00:00';",
        f"CREATE TABLE IF NOT EXISTS `{backup_table}` LIKE `characters`;",
        f"INSERT IGNORE INTO `{backup_table}` SELECT * FROM `characters` WHERE `is_public` = 1 AND `character_id` IN ({ids});",
        "START TRANSACTION;",
    ]
    for item in updates:
        lines.append(
            "UPDATE `characters` SET "
            f"`tags_json`={sql_string(item['tags_json'])}, "
            f"`personality`={sql_string(item['personality'])}, "
            f"`personality_tags`={sql_string(item['personality_tags'])}, "
            "`updated_at`=NOW(3) "
            f"WHERE `character_id`={sql_string(item['character_id'])} AND `is_public`=1;"
        )
    lines.extend([
        f"SELECT COUNT(*) AS target_count FROM `characters` WHERE `is_public`=1 AND `character_id` IN ({ids});",
        f"SELECT COUNT(*) AS backup_count FROM `{backup_table}` WHERE `character_id` IN ({ids});",
        "COMMIT;",
    ])
    return "\n".join(lines) + "\n"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--public", type=Path, default=Path("/tmp/pub1.json"))
    parser.add_argument("--source-dir", type=Path, default=Path("/Users/guoliang/Documents/日常对话/crushie_data"))
    parser.add_argument("--output-dir", type=Path, default=Path("scripts/generated"))
    args = parser.parse_args()

    public_items = load_public_items(args.public)
    if len(public_items) != 95:
        raise SystemExit(f"expected 95 public characters, got {len(public_items)}")
    source = args.source_dir / "crushie_80_translated.json"
    prompt = args.source_dir / "crushie_official_prompt_pack.json"
    updates, normalized = build_updates(public_items, source, prompt)
    if len(updates) != len(public_items):
        raise SystemExit(f"generated {len(updates)} updates for {len(public_items)} public characters")

    args.output_dir.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    sql_path = args.output_dir / f"discover_tag_remap_{stamp}.sql"
    manifest_path = args.output_dir / f"discover_tag_remap_{stamp}.json"
    backup_table = f"characters_discover_tags_backup_{stamp}"
    sql_path.write_text(generate_sql(updates, backup_table), encoding="utf-8")
    manifest_path.write_text(json.dumps(updates, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    backup = source.with_suffix(source.suffix + f".bak.{stamp}")
    shutil.copy2(source, backup)
    # Write only the 80 normalized Crushie records back, retaining file order.
    by_id = {item["character_id"]: item for item in normalized}
    original = json.loads(backup.read_text(encoding="utf-8"))
    for item in original:
        replacement = by_id.get(item.get("character_id"))
        if replacement:
            item.update({key: replacement[key] for key in ("tags_json", "personality", "personality_tags")})
    source.write_text(json.dumps(original, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    print(f"public characters: {len(updates)}")
    print(f"normalized Crushie records: {len(normalized)}")
    print(f"sql: {sql_path}")
    print(f"manifest: {manifest_path}")
    print(f"source backup: {backup}")
    print(f"backup table: {backup_table}")
    for category in DISCOVER_IDS:
        count = sum(category in json.loads(item["tags_json"]).get(group, []) for item in updates for group in GROUPS)
        print(f"{category}: {count}")


if __name__ == "__main__":
    main()
