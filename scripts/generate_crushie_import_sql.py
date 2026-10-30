#!/usr/bin/env python3
"""Generate an idempotent, backup-first MySQL import for localized characters."""

from __future__ import annotations

import argparse
import json
from datetime import datetime
from pathlib import Path
from typing import Any


def sql_string(value: Any) -> str:
    if value is None:
        return "NULL"
    text = str(value).replace("\\", "\\\\").replace("'", "''")
    text = text.replace("\r", "\\r").replace("\n", "\\n")
    return "'" + text + "'"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("input", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--backup-table", default=None)
    args = parser.parse_args()

    characters = json.loads(args.input.read_text(encoding="utf-8"))
    if len(characters) != 80:
        raise SystemExit(f"expected 80 characters, got {len(characters)}")
    backup_table = args.backup_table or "characters_crushie_backup_" + datetime.now().strftime("%Y%m%d_%H%M%S")
    ids = [sql_string(item["character_id"]) for item in characters]

    columns = [
        "created_at", "updated_at", "character_id", "user_id", "name", "gender", "age",
        "personality", "occupation", "background", "relationship", "style", "avatar",
        "is_public", "interaction_mode", "system_prompt", "comment", "narrative",
        "basic_setting", "example", "first_situation", "first_words", "character_type",
        "tags_json", "personality_tags",
    ]
    update_columns = [
        "updated_at", "user_id", "name", "gender", "age", "personality", "occupation",
        "background", "relationship", "style", "avatar", "is_public", "interaction_mode",
        "system_prompt", "comment", "narrative", "basic_setting", "example", "first_situation",
        "first_words", "character_type", "tags_json", "personality_tags",
    ]

    lines = [
        "SET NAMES utf8mb4;",
        "SET SESSION time_zone = '+00:00';",
        f"CREATE TABLE IF NOT EXISTS `{backup_table}` LIKE `characters`;",
        f"INSERT IGNORE INTO `{backup_table}` SELECT * FROM `characters` WHERE `character_id` IN ({','.join(ids)});",
        "START TRANSACTION;",
    ]
    for item in characters:
        values = [
            "NOW(3)", "NOW(3)", sql_string(item["character_id"]), sql_string("system"),
            sql_string(item["name"]), sql_string(item.get("gender", "male")), str(int(item.get("age", 25))),
            sql_string(item.get("personality", "[]")), sql_string(item.get("occupation", "")),
            sql_string(item.get("background", "")), sql_string(item.get("relationship", "伙伴")),
            sql_string(item.get("style", "温暖型")), sql_string(item.get("avatar", "")),
            "1", sql_string("active"), sql_string(item.get("system_prompt", "")),
            sql_string(item.get("comment", "")), sql_string(item.get("narrative", "")),
            sql_string(item.get("basic_setting", "")), sql_string(item.get("example", "")),
            sql_string(item.get("first_situation", "")), sql_string(item.get("first_words", "")),
            "0", sql_string(item.get("tags_json", "{}")), sql_string(item.get("personality_tags", "[]")),
        ]
        lines.append(
            "INSERT INTO `characters` (" + ",".join("`" + col + "`" for col in columns) + ") VALUES ("
            + ",".join(values) + ") ON DUPLICATE KEY UPDATE "
            + ",".join(f"`{col}`=VALUES(`{col}`)" for col in update_columns) + ";"
        )
    lines += [
        f"SELECT COUNT(*) AS imported_target_count FROM `characters` WHERE `character_id` IN ({','.join(ids)});",
        f"SELECT COUNT(*) AS backup_count FROM `{backup_table}` WHERE `character_id` IN ({','.join(ids)});",
        "COMMIT;",
    ]
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"generated {len(characters)} idempotent upserts -> {args.output}")
    print(f"backup table: {backup_table}")


if __name__ == "__main__":
    main()
