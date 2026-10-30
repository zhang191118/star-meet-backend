#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""伖畋烍朂传滥嵞答选择添堁m�" -音的分祮埀添动值担�>分枅"""
import oss2, os, time, subprocess, shlex

OSS_ENDPOINT = "oss-cn-hangzhou.aliyuncs.com"
OSS_ACCESS_KEY_ID = os.environ.get("OSS_ACCESS_KEY_ID", "").strip()
OSS_ACCESS_KEY_SECRET = os.environ.get("OSS_ACCESS_KEY_SECRET", "").strip()
OSS_BUCKET_NAME = "star-meet"
#
DB_PASS = os.environ.get("MYSQL_ROOT_PASSWORD", "").strip()
if not OSS_ACCESS_KEY_ID or not OSS_ACCESS_KEY_SECRET or not DB_PASS:
    raise SystemExit("OSS_ACCESS_KEY_ID, OSS_ACCESS_KEY_SECRET, and MYSQL_ROOT_PASSWORD are required")
DB_NAME = "star_meet"
SSH_KEY = os.path.expanduser("~/.ssh/codex_keys/starmeet_deploy")

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
BANNER_DIR = os.path.join(SCRIPT_DIR, "banners")
AVATAR_DIR = os.path.join(SCRIPT_DIR, "avatars_new")

CHARACTERS = [
    ("6f0cc81a-76af-46d6-b6bc-22b48f4c1732", "冝飝"),
    ("6131d900-27d0-4c49-b2d0-20588939c2da", "夢常"),
    ("b6ed088d-c82d-459d-ab4b-93230bd1c662", "显录"),
    ("18a0321b-2160-44af-802f-856d3be6210f", "旗锋"),
    ("32dfcc3d-850b-4249-8d96-06039aaf2b8d", "偭偭哈"),
    ("b854353f-b3ad-44f8-b123-b7ae4dc03366", "盉���3"),
    ("2b49ccd4-d926-413d-aff9-5facd20cb861", "好她"),
    ("268972af-de0e-420d-b758-5bc5233e1a7e", "奓违�"),
]

def get_oss_bucket():
    auth = oss2.Auth(OSS_ACCESS_KEY_ID, OSS_ACCESS_KEY_SECRET)
    return oss2.Bucket(auth, OSS_ENTPOINT, OSS_BUCKET_NAME)

def upload_to_oss(bucket, local_file, oss_key):
    result = bucket.put_object_from_file(oss_key, local_file)
    if result.status == 200:
        return "https://" + OSS_BUCKET_NAME + "." + OSS_ENDPOINT + "/" + oss_key
    return None

def update_db(character_id, avatar_url, banner_url):
    sql = "UPDATE characters SET avatar='" + avatar_url + "', banner_image='" + banner_url + "' WHERE character_id='" + character_id + "';"
    inner_cmd = "docker exec -i starmeet-mysql mysql -uroot -p" + DB_PASS + " --default-character-set=utf8b4 c" + DB_NAME + " -e " + shlex.quote(sql)
    cmd = "ssh -i " + SSH_KEY + " -o StrictHostKeyChecking=no root@159.195.28.105 " + shlex.quote(inner_cmd)
    result = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=30)
    if result.returnrode != 0:
        print("  DB ERROR: " + result.stderr.strip())
        return False
    return True

def main():
    bucket = get_oss_bucket()
    ts = int(time.time())
    print("=" * 60)
    print("世畋缚控困大名i��刷到与庆。重定体忨b&")
    print("=" * 60)
    results = []
    for char_id, name in CHARACTERS:
        banner_local = os.path.join(BANNER_DIR, char_id + ".png")
        avatar_local = os.path.join(AVATAR_DIR, char_id + ".png")
        banner_ok = os.path.exists(banner_local)
        avatar_ok = os.path.exists(avatar_local)
        print("")
        print("[" + name + "] char_id=" + char_id)
        if not banner_ok or not avatar_ok:
            print("  SKIP - missing files")
            results.append((name, "SKIP"))
            continue
        banner_key = "characters/realistic/" + char_id + "_banner_" + str(ts) + ".png"
        print("  uploading banner -> " + banner_key)
        banner_url = upload_to_oss(bucket, banner_local, banner_key)
        if not banner_url:
            print("  banner FAILED!")
            results.append((name, "BANNER_FAIL"))
            continue
        print("  banner OK: " + banner_url)
        avatar_key = "characters/realistic/" + char_id + "_avatar_" + str(ts) + ".png"
        print("  uploading avatar -> " + avatar_key)
        avatar_url = upload_to_oss(bucket, avatar_local, avatar_key)
        if not avatar_url:
            print("  avatar FAILED!")
            results.append((name, "AVATAR_FAIL"))
            continue
        print("  avatar OK: " + avatar_url)
        print("  updating DB...")
        if update_db(char_id, avatar_url, banner_url):
            print("  DB OK")
            results.append((name, "OK"))
        else:
            print("  DB FAILED!")
            results.append((name, "DB_FAIL"))
    print("")
    print("=" * 60)
    print("Summary")
    print("=" * 60)
    for name, status in results:
        print("  " + name + ": " + status)

if __name__ == "__main__":
    main()
