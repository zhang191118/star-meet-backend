#!/usr/bin/env python3
"""Upload the normalized Crushie avatar set without touching the database."""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.request
from pathlib import Path

import oss2


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("input", type=Path)
    parser.add_argument("--endpoint", default=os.environ.get("OSS_ENDPOINT", "oss-cn-hangzhou.aliyuncs.com"))
    parser.add_argument("--bucket", default=os.environ.get("OSS_BUCKET_NAME", ""))
    parser.add_argument("--access-key-id", default=os.environ.get("OSS_ACCESS_KEY_ID", ""))
    parser.add_argument("--access-key-secret", default=os.environ.get("OSS_ACCESS_KEY_SECRET", ""))
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()
    if not all((args.bucket, args.access_key_id, args.access_key_secret)):
        raise SystemExit("OSS credentials must be supplied through environment or arguments")

    characters = json.loads(args.input.read_text(encoding="utf-8"))
    auth = oss2.Auth(args.access_key_id, args.access_key_secret)
    bucket = oss2.Bucket(auth, args.endpoint, args.bucket)
    failed = []
    uploaded = 0
    skipped = 0
    for character in characters:
        local = Path(character["local_avatar"])
        key = f"characters/crushie/{character['character_id']}_avatar.png"
        try:
            exists = False
            if not args.force:
                try:
                    bucket.head_object(key)
                    exists = True
                except oss2.exceptions.NoSuchKey:
                    pass
            if exists:
                skipped += 1
                print(f"SKIP {character['character_id']} {character['name']}", flush=True)
            else:
                result = bucket.put_object_from_file(key, str(local))
                if result.status != 200:
                    raise RuntimeError(f"put status={result.status}")
                uploaded += 1
                print(f"UPLOADED {character['character_id']} {character['name']}", flush=True)

            url = f"https://{args.bucket}.{args.endpoint}/{key}"
            request = urllib.request.Request(url, method="HEAD")
            with urllib.request.urlopen(request, timeout=30) as response:
                if response.status != 200:
                    raise RuntimeError(f"public status={response.status}")
        except Exception as exc:  # noqa: BLE001
            failed.append((character["character_id"], character["name"], str(exc)))
            print(f"FAILED {character['character_id']} {character['name']} {exc}", flush=True)

    print(f"SUMMARY total={len(characters)} uploaded={uploaded} skipped={skipped} failed={len(failed)}", flush=True)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
