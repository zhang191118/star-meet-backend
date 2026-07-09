#!/usr/bin/env python3
import oss2
import os
import pymysql

# OSS配置
OSS_ENDPOINT = 'oss-cn-hangzhou.aliyuncs.com'
OSS_BUCKET_NAME = 'star-meet'
OSS_ACCESS_KEY_ID = os.getenv('OSS_ACCESS_KEY_ID', '')
OSS_ACCESS_KEY_SECRET = os.getenv('OSS_ACCESS_KEY_SECRET', '')

# 角色头像映射 (文件名hash -> 角色名 -> character_id)
CHARACTERS = {
    'c054d48a-a0cd-4ede-ac61-01bb841e495e.png': ('江寒', 'char_jianghan'),
    '3ac62d71-ab15-4187-b8d4-572ba609d451.png': ('于', 'char_yu'),
    '668ed1da-400a-441d-99cd-ef0723430cc7.png': ('甜甜', 'char_tiantian'),
    'f4179040-1bb0-4d1b-a2bb-338dc559ef85.png': ('念念', 'char_niannian'),
    'f7f39ce7-a7cc-403a-8166-84f0961cb237.png': ('娇娇', 'char_jiaojiao'),
    'b26110cd-ed8c-4289-8048-37a6c54f0d2d.png': ('小阳', 'char_xiaoyang'),
    '62eb23c9-9395-4f0d-bff0-ebe8adaa862b.png': ('辰', 'char_chen'),
    'a8b11530-034e-4cad-8918-8d1406ae7fd5.png': ('知知', 'char_zhizhi'),
    '12574a53-51c7-4883-8dcb-7ba7dd5365f5.png': ('烈', 'char_lie'),
    'f7702864-b2f2-475e-bf99-da466d273868.png': ('文', 'char_wen'),
    '813d7a87-785d-418b-886a-c76d7ef8d059.png': ('小阮', 'char_xiaoruan'),
    'e5f78cea-570d-4ac2-bb4d-08fb98536526.png': ('红秀', 'char_hongxiu'),
}

def get_oss_client():
    """从环境变量或配置文件获取OSS凭证"""
    import yaml
    config_path = os.getenv('STARMEET_CONFIG_PATH', '/Users/guoliang/www/gocode/meihuoai/star-meet-backend/config.yaml')
    with open(config_path, 'r') as f:
        config = yaml.safe_load(f)

    oss_config = config.get('oss', {})
    access_key_id = OSS_ACCESS_KEY_ID or oss_config.get('access_key_id')
    access_key_secret = OSS_ACCESS_KEY_SECRET or oss_config.get('access_key_secret')
    endpoint = os.getenv('OSS_ENDPOINT', oss_config.get('endpoint', OSS_ENDPOINT))
    bucket_name = os.getenv('OSS_BUCKET_NAME', oss_config.get('bucket_name', OSS_BUCKET_NAME))
    auth = oss2.Auth(access_key_id, access_key_secret)
    return oss2.Bucket(auth, endpoint, bucket_name)

def upload_to_oss(bucket, local_file, oss_key):
    """上传文件到OSS"""
    result = bucket.put_object_from_file(oss_key, local_file)
    if result.status == 200:
        return f"https://{OSS_BUCKET_NAME}.{OSS_ENDPOINT}/{oss_key}"
    return None

def update_db_avatar(character_id, avatar_url):
    """更新数据库中的头像URL"""
    import pymysql

    # 从配置文件获取数据库配置
    import yaml
    config_path = os.getenv('STARMEET_CONFIG_PATH', '/Users/guoliang/www/gocode/meihuoai/star-meet-backend/config.yaml')
    with open(config_path, 'r') as f:
        config = yaml.safe_load(f)

    db_config = config.get('database', {})
    conn = pymysql.connect(
        host=db_config.get('host', '127.0.0.1'),
        port=db_config.get('port', 3306),
        user=db_config.get('username', 'root'),
        password=db_config.get('password', ''),
        database=db_config.get('name', 'star_meet'),
        charset='utf8mb4'
    )

    cursor = conn.cursor()
    cursor.execute("UPDATE characters SET avatar = %s WHERE character_id = %s", (avatar_url, character_id))
    conn.commit()
    cursor.close()
    conn.close()
    print(f"Updated {character_id} with avatar: {avatar_url}")

def main():
    # 获取OSS客户端
    bucket = get_oss_client()

    # 图片目录
    img_dir = '/Users/guoliang/.openclaw/media/inbound'

    results = []

    for filename, (name, char_id) in CHARACTERS.items():
        local_path = os.path.join(img_dir, filename)
        if not os.path.exists(local_path):
            print(f"File not found: {local_path}")
            continue

        # OSS key
        oss_key = f"avatars/{char_id}_{filename}"

        print(f"Uploading {name} ({char_id})...")
        url = upload_to_oss(bucket, local_path, oss_key)

        if url:
            print(f"  Success: {url}")
            # 更新数据库
            update_db_avatar(char_id, url)
            results.append((name, char_id, url))
        else:
            print(f"  Failed!")

    print("\n=== Summary ===")
    for name, char_id, url in results:
        print(f"{name}: {url}")

if __name__ == '__main__':
    main()
