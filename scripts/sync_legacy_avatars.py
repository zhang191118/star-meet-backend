#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Upload reviewed legacy avatars and update only the 15 public source rows.

Required environment variables are intentionally external to the repository:
OSS_ENDPOINT, OSS_ACCESS_KEY_ID, OSS_ACCESS_KEY_SECRET, OSS_BUCKET_NAME and
STARMEET_SSH_KEY. The remote MySQL password is read inside the production
container and never passed through this process.
"""

from __future__ import annotations

import argparse
import json
import os
import shlex
import subprocess
import time
import urllib.request
from pathlib import Path

import oss2


HOST = os.environ.get("STARMEET_HOST", "159.195.28.105")
REMOTE_USER = os.environ.get("STARMEET_SSH_USER", "root")
SSH_KEY = os.environ.get("STARMEET_SSH_KEY", "")
ENDPOINT = os.environ.get("OSS_ENDPOINT", "oss-cn-hangzhou.aliyuncs.com")
BUCKET_NAME = os.environ.get("OSS_BUCKET_NAME", "star-meet")
ACCESS_KEY_ID = os.environ.get("OSS_ACCESS_KEY_ID", "")
ACCESS_KEY_SECRET = os.environ.get("OSS_ACCESS_KEY_SECRET", "")


def run_ssh(command: str, capture: bool = True) -> subprocess.CompletedProcess[str]:
    if not SSH_KEY:
        raise RuntimeError("STARMEET_SSH_KEY is empty")
    return subprocess.run(
        ["ssh", "-i", SSH_KEY, "-o", "StrictHostKeyChecking=no", f"{REMOTE_USER}@{HOST}", command],
        check=True,
        text=True,
        capture_output=capture,
        timeout=60,
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--check-only", action="store_true")
    parser.add_argument("--version", default=time.strftime("%Y%m%d%H%M%S"))
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    document = json.loads(args.manifest.read_text())
    entries = document["entries"]
    files = []
    for entry in entries:
        path = Path(entry["file"])
        if not path.is_absolute():
            path = args.manifest.parent / path
        if not path.exists() or path.stat().st_size < 20_000:
            raise RuntimeError(f"missing or too small: {path}")
        files.append((entry["character_id"], path))

    if args.check_only:
        print(f"validated {len(files)} avatar files")
        return 0

    if len(files) != 15:
        raise RuntimeError(f"refusing to sync {len(files)} files; expected exactly 15")
    if not ACCESS_KEY_ID or not ACCESS_KEY_SECRET:
        raise RuntimeError("OSS credentials are empty")

    # The backup is created before the first database write.
    backup_path = f"/opt/starmeet/backups/characters-before-legacy-avatar-{args.version}.sql"
    run_ssh(
        "mkdir -p /opt/starmeet/backups && "
        f"task_db_password=$(docker exec starmeet-mysql printenv MYSQL_ROOT_PASSWORD) && "
        "docker exec -e MYSQL_PWD=\"$task_db_password\" starmeet-mysql "
        f"mysqldump -uroot --single-transaction star_meet characters > {shlex.quote(backup_path)}"
    )
    print(f"backup: {backup_path}")

    bucket = oss2.Bucket(oss2.Auth(ACCESS_KEY_ID, ACCESS_KEY_SECRET), ENDPOINT, BUCKET_NAME)
    urls = {}
    for character_id, path in files:
        key = f"avatars/legacy-v2/{args.version}/{character_id}.png"
        result = bucket.put_object_from_file(key, str(path), headers={"Content-Type": "image/png"})
        if result.status != 200:
            raise RuntimeError(f"OSS upload failed for {character_id}: HTTP {result.status}")
        url = f"https://{BUCKET_NAME}.{ENDPOINT}/{key}"
        with urllib.request.urlopen(url, timeout=30) as response:
            if response.status != 200:
                raise RuntimeError(f"public URL check failed for {character_id}: HTTP {response.status}")
        urls[character_id] = url
        print(f"uploaded: {character_id}")

    cases = " ".join(
        f"WHEN character_id={shlex.quote(character_id)} THEN {shlex.quote(url)}"
        for character_id, url in urls.items()
    )
    ids = ",".join(shlex.quote(character_id) for character_id in urls)
    sql = (
        "START TRANSACTION; "
        f"UPDATE characters SET avatar=CASE {cases} END "
        f"WHERE character_id IN ({ids}) AND user_id='system_admin' AND is_public=1; "
        "SELECT ROW_COUNT(); COMMIT;"
    )
    command = (
        "task_db_password=$(docker exec starmeet-mysql printenv MYSQL_ROOT_PASSWORD) && "
        "docker exec -e MYSQL_PWD=\"$task_db_password\" starmeet-mysql "
        f"mysql -uroot --default-character-set=utf8mb4 -N -B star_meet -e {shlex.quote(sql)}"
    )
    result = run_ssh(command)
    affected = result.stdout.strip().splitlines()[-1]
    if affected != "15":
        raise RuntimeError(f"expected 15 source rows updated, got {affected}; restore {backup_path}")
    print("database: updated 15 public system rows")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
