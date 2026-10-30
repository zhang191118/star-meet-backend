#!/usr/bin/env python3
"""Generate backup-first SQL updates for character localized_profile JSON."""

from __future__ import annotations

import argparse
import json
from datetime import datetime
from pathlib import Path


def quote(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "''").replace("\r", "\\r").replace("\n", "\\n") + "'"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("input", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--backup-table", default="")
    parser.add_argument("--expected-count", type=int, default=95)
    args = parser.parse_args()
    rows = json.loads(args.input.read_text(encoding="utf-8"))
    if len(rows) != args.expected_count:
        raise SystemExit(f"expected {args.expected_count} localized profiles, got {len(rows)}")
    ids = [row["character_id"] for row in rows]
    if len(set(ids)) != len(ids):
        raise SystemExit("duplicate character_id in input")
    for row in rows:
        profile = row.get("localized_profile", {})
        if not all(profile.get(lang, {}).get(key, "").strip() for lang in ("en", "zh-Hant", "zh-Hans") for key in ("name", "narrative", "basic_setting")):
            raise SystemExit(f"incomplete profile for {row['character_id']}")

    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    backup = args.backup_table or f"characters_localized_profile_backup_{stamp}"
    target_list = ",".join(quote(value) for value in ids)
    lines = [
        "SET NAMES utf8mb4;",
        "SET SESSION time_zone = '+00:00';",
        "SET @localized_profile_exists = (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'characters' AND column_name = 'localized_profile');",
        "SET @localized_profile_ddl = IF(@localized_profile_exists = 0, 'ALTER TABLE `characters` ADD COLUMN `localized_profile` TEXT', 'SELECT 1');",
        "PREPARE localized_profile_ddl FROM @localized_profile_ddl;",
        "EXECUTE localized_profile_ddl;",
        "DEALLOCATE PREPARE localized_profile_ddl;",
        f"CREATE TABLE IF NOT EXISTS `{backup}` LIKE `characters`;",
        f"INSERT IGNORE INTO `{backup}` SELECT * FROM `characters` WHERE `is_public`=1 AND `character_id` IN ({target_list});",
        f"INSERT IGNORE INTO `{backup}` SELECT private_char.* FROM `characters` AS private_char JOIN `characters` AS public_char ON private_char.`character_id` = CONCAT(public_char.`character_id`, '_', LEFT(private_char.`user_id`, 8)) WHERE private_char.`is_public`=2 AND public_char.`is_public`=1 AND public_char.`character_id` IN ({target_list});",
        "START TRANSACTION;",
    ]
    for row in rows:
        value = json.dumps(row["localized_profile"], ensure_ascii=False, separators=(",", ":"))
        lines.append(f"UPDATE `characters` SET `localized_profile`={quote(value)}, `updated_at`=NOW(3) WHERE `is_public`=1 AND `character_id`={quote(row['character_id'])};")
    lines.extend([
        f"SELECT COUNT(*) AS target_count FROM `characters` WHERE `is_public`=1 AND `character_id` IN ({target_list});",
        f"SELECT COUNT(*) AS backup_count FROM `{backup}` WHERE `character_id` IN ({target_list});",
        f"UPDATE `characters` AS private_char JOIN `characters` AS public_char ON private_char.`character_id` = CONCAT(public_char.`character_id`, '_', LEFT(private_char.`user_id`, 8)) SET private_char.`localized_profile`=public_char.`localized_profile`, private_char.`updated_at`=NOW(3) WHERE private_char.`is_public`=2 AND public_char.`is_public`=1 AND public_char.`character_id` IN ({target_list});",
        "SELECT ROW_COUNT() AS copied_private_profiles;",
        "COMMIT;",
    ])
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"wrote {len(rows)} updates -> {args.output}")
    print(f"backup table: {backup}")


if __name__ == "__main__":
    main()
