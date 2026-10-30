#!/usr/bin/env python3
"""Upload generated Crushie 4K banner/bg images to OSS and emit SQL updates."""
from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.request
from pathlib import Path

import oss2


def public_url(bucket, endpoint, key):
    host = endpoint.replace('https://', '').replace('http://', '')
    return 'https://%s.%s/%s' % (bucket, host, key)


def load_oss_from_prod(args):
    import subprocess
    remote = (
        "set +H; set -a; . /opt/starmeet/star-meet-backend/.env >/dev/null 2>&1 || true; "
        "docker exec -i starmeet-mysql mysql -uroot -p\"$MYSQL_ROOT_PASSWORD\" "
        "--default-character-set=utf8mb4 -N star_meet -e "
        "\"SELECT config_key, value FROM runtime_configs WHERE config_key IN ('oss.access_key_id','oss.access_key_secret','oss.bucket_name','oss.endpoint');\""
    )
    cmd = [
        'ssh', '-i', str(Path.home() / '.ssh/codex_keys/starmeet_deploy'),
        '-o', 'StrictHostKeyChecking=no', 'root@159.195.28.105', remote,
    ]
    raw = subprocess.check_output(cmd, text=True)
    mapping = {}
    for line in raw.splitlines():
        line = line.strip()
        if not line or line.startswith('mysql:'):
            continue
        if '\t' in line:
            key, value = line.split('\t', 1)
        else:
            parts = line.split(None, 1)
            if len(parts) != 2:
                continue
            key, value = parts
        mapping[key.strip()] = value.strip()
    args.access_key_id = mapping.get('oss.access_key_id') or args.access_key_id
    args.access_key_secret = mapping.get('oss.access_key_secret') or args.access_key_secret
    args.bucket = mapping.get('oss.bucket_name') or args.bucket or 'star-meet'
    endpoint = mapping.get('oss.endpoint') or args.endpoint or 'oss-cn-hangzhou.aliyuncs.com'
    args.endpoint = endpoint.replace('https://', '').replace('http://', '')
    if not all((args.access_key_id, args.access_key_secret, args.bucket)):
        raise SystemExit('failed to load OSS credentials from production')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--input', type=Path, default=Path('/Users/guoliang/Documents/日常对话/crushie_data/crushie_80_translated.json'))
    parser.add_argument('--src', type=Path, default=Path('/Users/guoliang/www/gocode/meihuoai/star-meet-backend/scripts/generated/crushie_4k'))
    parser.add_argument('--sql-out', type=Path, default=None)
    parser.add_argument('--endpoint', default=os.environ.get('OSS_ENDPOINT', 'oss-cn-hangzhou.aliyuncs.com'))
    parser.add_argument('--bucket', default=os.environ.get('OSS_BUCKET_NAME', ''))
    parser.add_argument('--access-key-id', default=os.environ.get('OSS_ACCESS_KEY_ID', ''))
    parser.add_argument('--access-key-secret', default=os.environ.get('OSS_ACCESS_KEY_SECRET', ''))
    parser.add_argument('--force', action='store_true')
    parser.add_argument('--dry-run', action='store_true')
    parser.add_argument('--min-bytes', type=int, default=100000)
    parser.add_argument('--existing-only', action='store_true')
    parser.add_argument('--load-from-prod', action='store_true')
    args = parser.parse_args()
    if args.sql_out is None:
        args.sql_out = args.src / 'update_images.sql'
    if args.load_from_prod:
        load_oss_from_prod(args)
    if not args.dry_run and not all((args.bucket, args.access_key_id, args.access_key_secret)):
        raise SystemExit('OSS credentials must be supplied through environment or arguments')

    characters = json.loads(args.input.read_text(encoding='utf-8'))
    bucket = None
    if not args.dry_run:
        auth = oss2.Auth(args.access_key_id, args.access_key_secret)
        bucket = oss2.Bucket(auth, args.endpoint, args.bucket)

    uploaded = skipped = missing = failed = 0
    sql_lines = ['SET NAMES utf8mb4;']
    for ch in characters:
        cid = ch['character_id']
        name = ch.get('name')
        updates = []
        for kind, field in (('banner', 'banner_image'), ('bg', 'bg_image')):
            local = args.src / ('%s_%s.png' % (cid, kind))
            if not local.exists() or local.stat().st_size < args.min_bytes:
                missing += 1
                if not args.existing_only:
                    print('MISSING %s %s %s' % (cid, name, kind), flush=True)
                continue
            key = 'characters/crushie/%s_%s_4k.png' % (cid, kind)
            url = public_url(args.bucket or 'star-meet', args.endpoint, key)
            try:
                if args.dry_run:
                    print('DRY %s %s %s -> %s' % (cid, name, kind, key), flush=True)
                else:
                    exists = False
                    if not args.force:
                        try:
                            bucket.head_object(key)
                            exists = True
                        except oss2.exceptions.NoSuchKey:
                            exists = False
                    if exists:
                        skipped += 1
                        print('SKIP %s %s %s' % (cid, name, kind), flush=True)
                    else:
                        result = bucket.put_object_from_file(key, str(local))
                        if result.status not in (200, 203):
                            raise RuntimeError('put status=%s' % result.status)
                        uploaded += 1
                        print('UPLOADED %s %s %s' % (cid, name, kind), flush=True)
                    request = urllib.request.Request(url, method='HEAD')
                    with urllib.request.urlopen(request, timeout=30) as response:
                        if response.status != 200:
                            raise RuntimeError('public status=%s' % response.status)
                updates.append("%s='%s'" % (field, url.replace("'", "\\'")))
            except Exception as exc:
                failed += 1
                print('FAILED %s %s %s %s' % (cid, name, kind, exc), flush=True)
        if updates:
            sql_lines.append("UPDATE characters SET %s WHERE character_id='%s' AND is_public=1;" % (', '.join(updates), cid))

    args.sql_out.write_text('\n'.join(sql_lines) + '\n', encoding='utf-8')
    print('SUMMARY uploaded=%s skipped=%s missing=%s failed=%s sql=%s' % (uploaded, skipped, missing, failed, args.sql_out), flush=True)
    return 1 if failed else 0


if __name__ == '__main__':
    sys.exit(main())
