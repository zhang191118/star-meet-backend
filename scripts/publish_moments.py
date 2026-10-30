#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
角色动态每日发布脚本

流程：
  1. 从 MySQL 拉取官方公开角色（is_public=1）
  2. 跳过今天已发布过的角色（唯一键 character_id + publish_day）
  3. 调 OpenAI 兼容模型生成动态文案 + 图片搜索关键词
  4. 用关键词去 Pexels 搜图，下载后转存 OSS
  5. INSERT 一条 character_moments 记录

用法：
  ./run-publish-moments.sh                 # 正常发布今天所有角色的动态
  ./run-publish-moments.sh --dry-run       # 只打印将要生成的内容，不写库
  ./run-publish-moments.sh --char-id X     # 只发布指定角色（调试用）
  ./run-publish-moments.sh --immediate --limit 20  # 立即生成一批未发布角色

调度：服务器 crontab 建议在 08:00~22:00 每 10 分钟运行一次，保证每个角色每天都有机会发布
  */10 8-22 * * * cd /opt/star-meet/star-meet-backend/scripts && ./run-publish-moments.sh >> ../logs/moments.log 2>&1
"""

import argparse
import datetime
import difflib
import json
import os
import random
import re
import time
import urllib.parse
import urllib.request
from zoneinfo import ZoneInfo

import oss2
import pymysql

# ===== 配置 =====
# Secrets are supplied by the environment (or the production wrapper's
# .moments.env file), never kept in source code.
OSS_ENDPOINT = os.environ.get("MOMENTS_OSS_ENDPOINT", "oss-cn-hangzhou.aliyuncs.com")
OSS_ACCESS_KEY_ID = os.environ.get("OSS_ACCESS_KEY_ID", "")
OSS_ACCESS_KEY_SECRET = os.environ.get("OSS_ACCESS_KEY_SECRET", "")
OSS_BUCKET_NAME = os.environ.get("MOMENTS_OSS_BUCKET_NAME", "star-meet")

DB_HOST = os.environ.get("MOMENTS_DB_HOST", "127.0.0.1")
DB_PORT = int(os.environ.get("MOMENTS_DB_PORT", "3306"))
DB_USER = os.environ.get("MOMENTS_DB_USER", "root")
DB_PASS = os.environ.get("MOMENTS_DB_PASS", "")
DB_NAME = os.environ.get("MOMENTS_DB_NAME", "star_meet")

# The moments publisher uses the OpenAI-compatible endpoint by default. Keep
# the DashScope variables as a migration fallback for existing deployments.
MOMENTS_LLM_BASE_URL = os.environ.get(
    "MOMENTS_LLM_BASE_URL", "https://ai98pro.xyz/v1"
).rstrip("/")
MOMENTS_LLM_API_KEY = (
    os.environ.get("MOMENTS_LLM_API_KEY", "").strip()
    or os.environ.get("OPENAI_COMPAT_API_KEY", "").strip()
)
MOMENTS_LLM_MODEL = os.environ.get("MOMENTS_LLM_MODEL", "grok-4.6")

LEGACY_DASHSCOPE_API_KEY = os.environ.get("DASHSCOPE_API_KEY", "").strip()
LEGACY_DASHSCOPE_MODEL = os.environ.get("DASHSCOPE_MODEL", "qwen-turbo")
DASHSCOPE_URL = "https://dashscope.aliyuncs.com/api/v1/services/aigc/text-generation/generation"

PEXELS_API_KEY = os.environ.get("PEXELS_API_KEY", "")
PEXELS_SEARCH_URL = "https://api.pexels.com/v1/search"
LLM_REQUEST_TIMEOUT = float(os.environ.get("MOMENTS_LLM_TIMEOUT", "180"))

PUBLISH_HOUR_MIN = 8
PUBLISH_HOUR_MAX = 22
PUBLISH_MAX_PER_RUN = int(os.environ.get("MOMENTS_MAX_PER_RUN", "20"))
IMAGES_PER_MOMENT = 1
PUBLISH_TZ_NAME = os.environ.get("MOMENTS_TZ", "Asia/Taipei")
PUBLISH_TZ = ZoneInfo(PUBLISH_TZ_NAME)
MOMENTS_LOCALE = os.environ.get("MOMENTS_LOCALE", "zh-Hant-TW")


def now_local():
    return datetime.datetime.now(PUBLISH_TZ)


def to_utc_naive(dt):
    """Store DATETIME in UTC so the API container (loc=Local, currently UTC) yields the correct instant."""
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=PUBLISH_TZ)
    return dt.astimezone(datetime.timezone.utc).replace(tzinfo=None)


def log(msg):
    now = now_local().strftime("%Y-%m-%d %H:%M:%S")
    print("[" + now + "] " + msg, flush=True)


# ---------- DB ----------
def get_db():
    return pymysql.connect(
        host=DB_HOST, port=DB_PORT, user=DB_USER, password=DB_PASS,
        database=DB_NAME, charset="utf8mb4",
        cursorclass=pymysql.cursors.DictCursor,
    )


def fetch_public_characters(conn, only_char_id=None):
    sql = (
        "SELECT character_id, name, age, gender, occupation, personality, "
        "background, narrative, basic_setting, example, hobbies, speech_tone, "
        "relationship, style, weaknesses, catchphrases, daily_life, comment, "
        "first_situation, first_words, personality_tags, interaction_mode "
        "FROM characters "
        "WHERE is_public = 1 AND deleted_at IS NULL"
    )
    params = []
    if only_char_id:
        sql += " AND character_id = %s"
        params.append(only_char_id)
    sql += " ORDER BY character_id ASC"
    with conn.cursor() as cur:
        cur.execute(sql, params)
        return cur.fetchall()


def already_published_today(conn, character_id, publish_day):
    with conn.cursor() as cur:
        cur.execute(
            "SELECT 1 FROM character_moments WHERE character_id=%s AND publish_day=%s LIMIT 1",
            (character_id, publish_day),
        )
        return cur.fetchone() is not None


def fetch_recent_moments(conn, character_id, limit=10):
    """Load recent posts so the editor can vary hooks, scenes, and emotions."""
    with conn.cursor() as cur:
        cur.execute(
            "SELECT content FROM character_moments "
            "WHERE character_id=%s AND status=1 AND deleted_at IS NULL "
            "ORDER BY published_at DESC, id DESC LIMIT %s",
            (character_id, limit),
        )
        return [str(row.get("content") or "").strip() for row in cur.fetchall() if row.get("content")]


def insert_moment(conn, character_id, content, image_urls, tags, publish_day, published_at):
    with conn.cursor() as cur:
        cur.execute(
            "INSERT INTO character_moments "
            "(created_at, updated_at, character_id, content, image_urls, topic_tags, "
            " like_count, status, publish_day, published_at) "
            "VALUES (NOW(), NOW(), %s, %s, %s, %s, 0, 1, %s, %s)",
            (
                character_id,
                content,
                json.dumps(image_urls, ensure_ascii=False),
                ",".join(tags),
                publish_day,
                to_utc_naive(published_at).strftime("%Y-%m-%d %H:%M:%S"),
            ),
        )
    conn.commit()


# ---------- LLM ----------
PROMPT_TEMPLATE = """你是一个面向台湾年轻女性的虚拟角色社交信息流编辑。目标不是写工作汇报，而是写一条用户在无聊刷动态时会停下来看的内容，让人产生好奇、共鸣，或想点进聊天了解后续。

角色资料：
姓名：{name}；年龄：{age}；性别：{gender_text}；职业：{occupation}
关系定位：{relationship}；互动风格：{style}
性格：{personality}；语气：{speech_tone}；口头禅：{catchphrases}
爱好：{hobbies}；日常生活：{daily_life}
弱点：{weaknesses}
角色卡钩子：{comment}
履历：{basic_setting}
背景：{background}
故事：{narrative_line}
开场情境：{first_situation}
说话示例：{example}
日期：{date_str}（{weekday_str}）{festival_line}
目标语言：{locale}，请使用自然的台湾繁體中文，不要生硬直译或堆砌网络流行语。

最近动态（只用于避免重复，不要照抄）：
{recent_moments}

请先选择一个最适合今天的内容类型：fragment（碎片动态）、confession（倾诉）、relationship_scene（关系现场）、lifestyle（生活方式）、serial（连续事件）。再生成 3 条不同方向的候选，每条 45~120 字。

每条候选必须满足：
1. 有一个具体可想象的细节，例如时间、物件、动作、声音、天气、聊天记录或一个小场景。
2. 有情绪反差、未解释的信息，或一个轻微关系张力，让人想知道“后来呢”。
3. 留下自然回复入口，但不要每条都直接问“你在吗”，也不要假定用户已经和角色恋爱。
4. 必须像角色本人发的动态，不能像广告、鸡汤、剧情简介或刻意写给用户的情话。
5. 可以暧昧、失落、吃醋、嘴硬、需要安慰，但禁止色情、情绪勒索、自残暗示、虚假重大危机或把用户写成唯一救命稻草。
6. 不得新增资料中没有的身份、经历、地点、人物关系或重大事件。最多使用 1 个 emoji。

给每条候选附上简短元数据：format、hook_type、emotion_type、reply_intent、quality_score（0~10）。另给 2~3 个英文无人物写实摄影搜图词，必须是场景、物件、食物、风景、室内、天气或桌面细节。
只输出 JSON：{{"candidates":[{{"content":"...","format":"fragment","hook_type":"留白","emotion_type":"嘴硬","reply_intent":"想问发生了什么","quality_score":8,"tags":["标签"],"image_keywords":["rainy office window"]}}]}}"""

WEEKDAYS = ["周一", "周二", "周三", "周四", "周五", "周六", "周日"]

FESTIVALS = {
    "01-01": "元旦",
    "02-14": "情人节",
    "03-08": "妇女节",
    "05-01": "劳动节",
    "06-01": "儿童节",
    "10-01": "国庆节",
    "12-25": "圣诞节",
    "12-31": "跨年夜",
}


def build_prompt(char, today, recent_moments=None):
    gender_text = {"male": "男性", "female": "女性", "other": ""}.get(char.get("gender") or "", "")
    festival = FESTIVALS.get(today.strftime("%m-%d"))
    festival_line = "，今天是" + festival if festival else ""
    recent_lines = "\n".join("- " + item[:160] for item in (recent_moments or [])) or "（暂无历史动态）"

    return PROMPT_TEMPLATE.format(
        name=char.get("name") or "未知",
        age=char.get("age") or 22,
        gender_text=gender_text,
        occupation=char.get("occupation") or "自由职业",
        relationship=char.get("relationship") or "伙伴",
        style=char.get("style") or char.get("interaction_mode") or "温暖型",
        personality=(char.get("personality") or "").replace("\n", "，")[:140],
        speech_tone=(char.get("speech_tone") or "自然、生活化")[:80],
        catchphrases=(char.get("catchphrases") or "")[:100],
        hobbies=(char.get("hobbies") or "")[:100],
        daily_life=(char.get("daily_life") or "")[:180],
        weaknesses=(char.get("weaknesses") or "")[:120],
        comment=(char.get("comment") or "")[:120],
        basic_setting=(char.get("basic_setting") or "")[:220],
        background=(char.get("background") or "")[:160],
        narrative_line=(char.get("narrative") or "")[:220],
        first_situation=(char.get("first_situation") or "")[:180],
        example=(char.get("example") or "")[:180],
        date_str=today.strftime("%Y年%m月%d日"),
        weekday_str=WEEKDAYS[today.weekday()],
        festival_line=festival_line,
        locale=MOMENTS_LOCALE,
        recent_moments=recent_lines,
    )


LLM_META_WORDS = ["作为AI", "虚拟角色", "语言模型", "根据资料", "提示词"]
PERSON_IMAGE_WORDS = (
    "person", "people", "portrait", "face", "headshot", "selfie", "man",
    "men", "woman", "women", "girl", "boy", "human", "couple", "friend",
    "friends", "crowd", "model", "lady", "guy", "child", "children", "kid",
    "baby", "adult", "teen", "male", "female", "pedestrian", "worker",
    "chef", "waiter", "actress", "actor", "someone", "somebody",
)
CHINESE_PERSON_MARKERS = (
    "人像", "肖像", "自拍", "人物", "脸部", "面部", "美女", "帅哥",
    "女孩", "男孩", "女人", "男人", "人群", "模特", "女生", "男生",
    "少女", "少年", "人们",
)
PERSON_IMAGE_PATTERNS = tuple(
    re.compile(r"\b" + re.escape(word) + r"s?\b", re.I)
    for word in PERSON_IMAGE_WORDS
)


def looks_like_person_text(text):
    """True when a search term or photo caption is about people."""
    value = str(text or "").strip()
    if not value:
        return False
    if any(marker in value for marker in CHINESE_PERSON_MARKERS):
        return True
    # Keep empty-scene captions like "street with no people".
    cleaned = re.sub(
        r"\b(?:no|without)\s+(?:a\s+)?(?:people|persons?|crowd)\b",
        " ",
        value,
        flags=re.I,
    )
    return any(pattern.search(cleaned) for pattern in PERSON_IMAGE_PATTERNS)


def sanitize_image_keyword(keyword):
    """Keep only non-person scene/object keywords."""
    value = str(keyword or "").strip()
    if not value or looks_like_person_text(value):
        return ""
    return value


def score_candidate(candidate, recent_moments):
    """Prefer concrete, unfinished, replyable posts while avoiding repetition."""
    content = candidate["content"]
    try:
        score = float(candidate.get("quality_score") or 0)
    except (TypeError, ValueError):
        score = 0.0
    if 45 <= len(content) <= 120:
        score += 1.5
    if any(mark in content for mark in ("…", "……", "？", "?", "。")):
        score += 0.5
    if any(mark in content for mark in ("其實", "只是", "算了", "結果", "卻", "偏偏", "沒說")):
        score += 1.0
    if any(mark in content for mark in ("你會", "如果是你", "怎麼辦", "要不要", "後來", "想問")):
        score += 0.8
    if any(re.search(pattern, content) for pattern in (r"\d", r"凌晨|早上|晚上|今天|剛剛", r"手機|門|燈|杯|雨|訊息|桌")):
        score += 0.8
    if any(phrase in content for phrase in ("今天陽光很好", "開心的一天", "努力生活", "一切都會好")):
        score -= 2.0
    for previous in recent_moments or []:
        similarity = difflib.SequenceMatcher(None, content, previous).ratio()
        if similarity >= 0.72:
            score -= 4.0
        elif similarity >= 0.58:
            score -= 1.5
    return score


def parse_llm_result(text, recent_moments=None):
    """Parse and validate one JSON response from the LLM."""
    if not text:
        return None

    # Models occasionally wrap valid JSON in a markdown code fence.
    text = re.sub(r"^\s*```(?:json)?\s*|\s*```\s*$", "", text.strip(), flags=re.IGNORECASE)
    start = text.find("{")
    if start < 0:
        log("  LLM 输出中未找到 JSON: " + text[:200])
        return None
    try:
        data, _ = json.JSONDecoder().raw_decode(text[start:])
    except json.JSONDecodeError as e:
        log("  JSON 解析失败: " + str(e))
        return None
    if not isinstance(data, dict):
        log("  LLM JSON 不是对象")
        return None

    raw_candidates = data.get("candidates")
    if not isinstance(raw_candidates, list):
        raw_candidates = [data]

    candidates = []
    for raw_candidate in raw_candidates[:5]:
        if not isinstance(raw_candidate, dict):
            continue
        content = str(raw_candidate.get("content") or "").strip()
        if len(content) < 45 or len(content) > 160:
            log("  候选动态长度不合要求: " + str(len(content)))
            continue
        if any(word in content for word in LLM_META_WORDS):
            log("  候选动态包含模型元话语，拒绝候选")
            continue

        tags = raw_candidate.get("tags") or []
        keywords = raw_candidate.get("image_keywords") or []
        if not isinstance(tags, list):
            tags = []
        if not isinstance(keywords, list):
            keywords = []
        sanitized = []
        for keyword in keywords:
            raw_keyword = str(keyword).strip()
            if not raw_keyword:
                continue
            cleaned = sanitize_image_keyword(raw_keyword)
            if not cleaned:
                log("  丢弃人物搜图词: " + raw_keyword)
                continue
            if cleaned not in sanitized:
                sanitized.append(cleaned)
        if not sanitized:
            continue
        candidate = {
            "content": content,
            "tags": [str(t).strip() for t in tags if str(t).strip()][:3],
            "image_keywords": sanitized[:3],
            "format": str(raw_candidate.get("format") or "fragment").strip()[:32],
            "hook_type": str(raw_candidate.get("hook_type") or "留白").strip()[:32],
            "emotion_type": str(raw_candidate.get("emotion_type") or "日常").strip()[:32],
            "reply_intent": str(raw_candidate.get("reply_intent") or "想了解后续").strip()[:80],
            "quality_score": raw_candidate.get("quality_score") or 0,
        }
        candidate["_score"] = score_candidate(candidate, recent_moments or [])
        candidates.append(candidate)

    if not candidates:
        log("  没有可用的动态候选")
        return None
    selected = max(candidates, key=lambda item: item["_score"])
    selected.pop("_score", None)
    return selected


def call_llm(prompt, recent_moments=None):
    """Call the configured OpenAI-compatible model, with legacy fallback."""
    use_openai_compat = bool(MOMENTS_LLM_API_KEY)
    if not use_openai_compat and not LEGACY_DASHSCOPE_API_KEY:
        log("  WARN: MOMENTS_LLM_API_KEY 未配置，跳过 LLM")
        return None

    request_prompt = prompt
    for attempt in range(2):
        if use_openai_compat:
            endpoint = MOMENTS_LLM_BASE_URL + "/chat/completions"
            payload = {
                "model": MOMENTS_LLM_MODEL,
                "messages": [{"role": "user", "content": request_prompt}],
                "temperature": 0.75,
                # Short social posts do not need a large completion budget;
                # keeping this bounded prevents a slow reasoning model from
                # delaying the daily batch.
                "max_tokens": 900,
            }
            api_key = MOMENTS_LLM_API_KEY
        else:
            endpoint = DASHSCOPE_URL
            payload = {
                "model": LEGACY_DASHSCOPE_MODEL,
                "input": {"messages": [{"role": "user", "content": request_prompt}]},
                "parameters": {"temperature": 0.9, "top_p": 0.8, "max_tokens": 1200},
            }
            api_key = LEGACY_DASHSCOPE_API_KEY

        req = urllib.request.Request(
            endpoint,
            data=json.dumps(payload).encode("utf-8"),
            headers={
                "Content-Type": "application/json",
                "Accept": "application/json",
                "Authorization": "Bearer " + api_key,
            },
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=LLM_REQUEST_TIMEOUT) as resp:
                body = json.loads(resp.read().decode("utf-8"))
        except Exception as e:
            log("  LLM 请求失败: " + str(e))
            return None

        if use_openai_compat:
            choices = body.get("choices") or []
            message = choices[0].get("message") if choices else {}
            text = (message or {}).get("content") or ""
            if isinstance(text, list):
                text = "".join(
                    str(part.get("text") or "") if isinstance(part, dict) else str(part)
                    for part in text
                )
        else:
            text = (body.get("output") or {}).get("text") or ""
        if not text:
            log("  LLM 返回空内容: " + json.dumps(body, ensure_ascii=False)[:300])
            return None

        result = parse_llm_result(text, recent_moments)
        if result:
            return result
        if attempt == 0:
            request_prompt += "\n上一轮输出不符合要求。请只输出合法 JSON 对象，必须包含 3 条不同候选，每条 content 为 45~120 字，并附 format、hook_type、emotion_type、reply_intent、quality_score、tags、image_keywords。搜图词必须是无人的场景、物件、食物、风景、室内或桌面细节，禁止人物、肖像、脸部、自拍、人群、身体或模特。"

    return None


# ---------- 敏感词兜底 ----------
BANNED_WORDS = ["约炮", "裸聊", "政治", "领导人"]


def content_safe(text):
    for w in BANNED_WORDS:
        if w in text:
            return False
    return True


# ---------- Pexels 搜图 + OSS 转存 ----------
def search_pexels(keyword, per_page=8):
    query = urllib.parse.urlencode({"query": keyword, "per_page": per_page, "orientation": "landscape"})
    req = urllib.request.Request(
        PEXELS_SEARCH_URL + "?" + query,
        headers={
            "Authorization": PEXELS_API_KEY,
            "User-Agent": "StarMeet-Moments/1.0",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            body = json.loads(resp.read().decode("utf-8"))
    except Exception as e:
        log("  Pexels 搜索失败 [" + keyword + "]: " + str(e))
        return []

    photos = body.get("photos") or []
    urls = []
    for p in photos:
        alt = str(p.get("alt") or "").strip()
        if looks_like_person_text(alt):
            log("  跳过人物配图 [" + keyword + "]: " + alt[:80])
            continue
        src = p.get("src") or {}
        url = src.get("large") or src.get("medium") or src.get("original")
        if url:
            urls.append(url)
    return urls


def download_image(url, timeout=20):
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "StarMeet-Moments/1.0"})
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.read()
    except Exception as e:
        log("  图片下载失败: " + str(e))
        return None


def get_oss_bucket():
    auth = oss2.Auth(OSS_ACCESS_KEY_ID, OSS_ACCESS_KEY_SECRET)
    return oss2.Bucket(auth, OSS_ENDPOINT, OSS_BUCKET_NAME)


def upload_to_oss(bucket, data, oss_key):
    try:
        result = bucket.put_object(oss_key, data)
        if result.status == 200:
            return "https://" + OSS_BUCKET_NAME + "." + OSS_ENDPOINT + "/" + oss_key
    except Exception as e:
        log("  OSS 上传失败: " + str(e))
    return None


def fetch_images_for_moment(bucket, keywords, character_id, publish_day):
    oss_urls = []
    seen_urls = set()

    search_keywords = []
    for kw in keywords or []:
        raw = str(kw).strip()
        if not raw:
            continue
        cleaned = sanitize_image_keyword(raw)
        if not cleaned:
            log("  跳过人物搜图词: " + raw)
            continue
        if cleaned not in search_keywords:
            search_keywords.append(cleaned)

    for kw in search_keywords:
        if len(oss_urls) >= IMAGES_PER_MOMENT:
            break
        candidates = search_pexels(kw)
        for url in candidates:
            if len(oss_urls) >= IMAGES_PER_MOMENT:
                break
            if url in seen_urls:
                continue
            seen_urls.add(url)
            data = download_image(url)
            if not data or len(data) < 10 * 1024:
                continue
            ts = int(time.time())
            oss_key = "moments/" + character_id + "/" + publish_day + "_" + str(ts) + ".jpg"
            oss_url = upload_to_oss(bucket, data, oss_key)
            if oss_url:
                oss_urls.append(oss_url)
                log("  配图 OK: " + oss_url)

    return oss_urls


# ---------- 主流程 ----------
def scheduled_publish_time(today, character_index, character_count):
    """Assign each character a stable slot across today's publish window."""
    start = PUBLISH_HOUR_MIN * 60
    window = max(1, (PUBLISH_HOUR_MAX - PUBLISH_HOUR_MIN) * 60)
    if character_count <= 1:
        offset = 0
    else:
        offset = int(((character_index + 0.5) * window) / character_count)
    return datetime.datetime.combine(today, datetime.time(), tzinfo=PUBLISH_TZ) + datetime.timedelta(
        minutes=start + offset
    )


def process_character(conn, bucket, char, today, dry_run, published_at=None):
    char_id = char["character_id"]
    name = char.get("name") or char_id
    publish_day = today.strftime("%Y-%m-%d")

    if already_published_today(conn, char_id, publish_day):
        log("[" + name + "] 今天已发布，跳过")
        return "SKIP_EXISTS"

    recent_moments = fetch_recent_moments(conn, char_id)
    prompt = build_prompt(char, today, recent_moments)
    llm_result = call_llm(prompt, recent_moments)
    if not llm_result:
        log("[" + name + "] LLM 超时或返回不合格，本轮跳过，等待下一轮")
        return "LLM_FAIL"

    content = llm_result["content"]
    if not content_safe(content):
        log("[" + name + "] 内容命中敏感词，跳过: " + content[:50])
        return "BANNED"

    tags = llm_result["tags"]
    keywords = llm_result["image_keywords"]
    published_at = published_at or now_local().replace(second=0, microsecond=0)

    log("[" + name + "] 文案: " + content[:80] + ("..." if len(content) > 80 else ""))
    log(
        "[" + name + "] 类型: " + str(llm_result.get("format") or "fragment")
        + " / 钩子: " + str(llm_result.get("hook_type") or "留白")
        + " | 标签: " + ",".join(tags)
        + " | 关键词: " + ",".join(keywords)
    )

    if dry_run:
        log("[" + name + "] DRY-RUN，不写库")
        return "DRY_RUN"

    image_urls = fetch_images_for_moment(bucket, keywords, char_id, publish_day)
    if not image_urls:
        log("[" + name + "] 未获取到模型关键词对应的配图，本轮跳过，等待下一轮")
        return "IMAGE_FAIL"

    try:
        insert_moment(conn, char_id, content, image_urls, tags, publish_day, published_at)
        log("[" + name + "] 发布成功")
        return "OK"
    except Exception as e:
        log("[" + name + "] 入库失败: " + str(e))
        return "DB_FAIL"


def main():
    parser = argparse.ArgumentParser(description="角色动态每日发布")
    parser.add_argument("--dry-run", action="store_true", help="只打印不写库")
    parser.add_argument("--char-id", default="", help="只发布指定角色")
    parser.add_argument("--immediate", action="store_true", help="忽略时间槽，立即处理未发布角色")
    parser.add_argument("--limit", type=int, default=0, help="本次最多处理数量，默认读取 MOMENTS_MAX_PER_RUN")
    args = parser.parse_args()

    now = now_local()
    today = now.date()
    log("=" * 60)
    log(
        "开始发布角色动态，日期="
        + today.strftime("%Y-%m-%d")
        + " 时区="
        + PUBLISH_TZ_NAME
        + ("（DRY-RUN）" if args.dry_run else "")
    )
    log("=" * 60)

    missing = []
    for name, value in (
        ("MOMENTS_DB_PASS", DB_PASS),
        ("PEXELS_API_KEY", PEXELS_API_KEY),
        ("OSS_ACCESS_KEY_ID", OSS_ACCESS_KEY_ID),
        ("OSS_ACCESS_KEY_SECRET", OSS_ACCESS_KEY_SECRET),
    ):
        if not value:
            missing.append(name)
    if not MOMENTS_LLM_API_KEY and not LEGACY_DASHSCOPE_API_KEY:
        missing.append("MOMENTS_LLM_API_KEY")
    if missing:
        raise SystemExit("缺少发布配置: " + ", ".join(missing) + "。请配置 .moments.env 或环境变量。")

    conn = get_db()
    bucket = get_oss_bucket()

    characters = fetch_public_characters(conn, only_char_id=args.char_id or None)
    log("共 " + str(len(characters)) + " 个官方公开角色")

    results = {}
    publish_day = today.strftime("%Y-%m-%d")
    due_characters = []
    for index, char in enumerate(characters):
        scheduled_at = scheduled_publish_time(today, index, len(characters))
        if already_published_today(conn, char["character_id"], publish_day):
            continue
        if args.immediate or args.char_id or scheduled_at <= now:
            due_characters.append((index, char, scheduled_at))

    max_per_run = args.limit if args.limit > 0 else PUBLISH_MAX_PER_RUN
    if not args.char_id and len(due_characters) > max_per_run:
        log("本次待处理 " + str(len(due_characters)) + " 个角色，最多处理 " + str(max_per_run) + " 个")
        due_characters = due_characters[:max_per_run]
    elif not due_characters:
        log("本次没有到发布时间的角色，等待下一轮")

    for index, char, scheduled_at in due_characters:
        status = process_character(
            conn, bucket, char, today, args.dry_run,
            published_at=scheduled_at if not (args.char_id or args.immediate) else now.replace(second=0, microsecond=0),
        )
        results[char.get("name") or char["character_id"]] = status
        time.sleep(random.uniform(1.0, 3.0))

    conn.close()

    log("=" * 60)
    log("汇总")
    log("=" * 60)
    for name, status in results.items():
        log("  " + name + ": " + status)


if __name__ == "__main__":
    main()
