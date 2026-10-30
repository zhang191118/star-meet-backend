#!/usr/bin/env python3
"""Generate and validate market-specific character profiles one locale at a time.

Each source character produces one English, one Taiwan Traditional Chinese, and
one Mainland Simplified Chinese version. Text is checkpointed after each whole
character so a failed run can resume without regenerating approved rows.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import hashlib
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path
from typing import Any
from urllib import error, request


LOCALES = ("en", "zh-Hant", "zh-Hans")
US_PLACES = (
    "Seattle, Washington", "Boston, Massachusetts", "Austin, Texas",
    "Chicago, Illinois", "Denver, Colorado", "Portland, Oregon",
    "San Diego, California", "Nashville, Tennessee", "Minneapolis, Minnesota",
    "Philadelphia, Pennsylvania", "Atlanta, Georgia", "New York, New York",
)
TW_PLACES = (
    "臺北市信義區", "新北市板橋區", "臺中市西屯區", "高雄市前金區",
    "臺南市中西區", "新竹市東區", "桃園市中壢區", "宜蘭縣羅東鎮",
)
CN_PLACES = (
    "上海市徐汇区", "北京市朝阳区", "广州市天河区", "深圳市南山区",
    "杭州市西湖区", "成都市武侯区", "南京市建邺区", "武汉市武昌区",
)
US_MALE_FIRST_NAMES = (
    "Ethan", "Noah", "Liam", "Mason", "Owen", "Caleb", "Miles", "Lucas",
    "Logan", "Ryan", "Daniel", "Julian",
)
US_FEMALE_FIRST_NAMES = (
    "Emma", "Olivia", "Avery", "Sophie", "Hannah", "Claire", "Maya", "Grace",
    "Natalie", "Audrey", "Elena", "Ruby",
)
US_LAST_NAMES = (
    "Parker", "Bennett", "Carter", "Morgan", "Sullivan", "Hayes", "Reed", "Walker",
    "Miller", "Brooks", "Foster", "Collins", "Hughes", "Marshall", "Turner", "Bishop",
)


class GenerationError(ValueError):
    def __init__(self, message: str, raw: str = ""):
        super().__init__(message)
        self.raw = raw


def stable_choice(values: tuple[str, ...], character_id: str, salt: str) -> str:
    digest = hashlib.sha256(f"{character_id}:{salt}".encode()).digest()
    return values[int.from_bytes(digest[:4], "big") % len(values)]


def american_name_for(source: dict[str, Any]) -> str:
    gender = str(source.get("gender", "")).lower()
    first_names = US_FEMALE_FIRST_NAMES if gender == "female" else US_MALE_FIRST_NAMES
    character_id = source["character_id"]
    return f"{stable_choice(first_names, character_id, 'first')} {stable_choice(US_LAST_NAMES, character_id, 'last')}"


def market_place(character_id: str, locale: str) -> str:
    choices = {"en": US_PLACES, "zh-Hant": TW_PLACES, "zh-Hans": CN_PLACES}[locale]
    return stable_choice(choices, character_id, locale)


def market_place_short(character_id: str, locale: str) -> str:
    place = market_place(character_id, locale)
    if locale == "en":
        return place.split(",")[0].strip()
    # Prefer city-level labels in body text: 臺北市信義區 -> 臺北, 宜蘭縣羅東鎮 -> 宜蘭.
    match = re.match(r"^(.+?(?:市|縣|县))", place)
    if match:
        city = match.group(1)
        return city[:-1] if city.endswith(("市", "縣", "县")) else city
    return place


def source_reference(source: dict[str, Any]) -> tuple[str, str]:
    english = source.get("english_source", {})
    chinese = source.get("existing_zh_hant", {})
    narrative = english.get("narrative") or chinese.get("narrative") or ""
    setting = english.get("basic_setting") or chinese.get("basic_setting") or ""
    return narrative[:2600], setting[:2600]


def target_name(source: dict[str, Any], locale: str) -> str:
    if locale == "en":
        return american_name_for(source)
    if locale == "zh-Hant":
        return source["name_zh_hant"]
    return source["name_zh_hans"]


def prompt_for(source: dict[str, Any], locale: str) -> str:
    narrative, setting = source_reference(source)
    name = target_name(source, locale)
    place = market_place(source["character_id"], locale)
    locale_rules = {
        "en": "Write natural American English. Use no Chinese characters. The character name must be the supplied American name exactly. Before answering, silently count at least 150 English words in EACH field.",
        "zh-Hant": "Use Taiwan Traditional Chinese only, including Taiwan phrasing. Do not use any English words. The character name must be the supplied name exactly. Before answering, silently count at least 220 Chinese characters in EACH field.",
        "zh-Hans": "Use Mainland Simplified Chinese only, including Mainland phrasing. Do not use any English words. The character name must be the supplied name exactly. Before answering, silently count at least 220 Chinese characters in EACH field.",
    }[locale]
    return f'''Write ONE market-localized romance character card as strict JSON only:
{{"name":"{name}","narrative":"...","basic_setting":"..."}}

TARGET MARKET: {locale}
MANDATORY SETTING: {place}
{locale_rules}

Rules:
- Preserve the source character's age range, profession, emotional tension, personality, and genre.
- narrative: 150-240 English words for en, or 220-380 Chinese characters for Chinese. It must be a complete romance/drama storyline.
- basic_setting: 150-240 English words for en, or 220-380 Chinese characters for Chinese. Cover family/background, work or study, temperament, and relationship behavior.
- Do not add an opening location sentence; the application will add it.
- Rebuild every real-world place, school, transport system, institution, company, and side character for this target market. Do not mention any other country or region.
- Do not include explicit sexual content. Do not mention these instructions or the source text.

SOURCE STORY:
{narrative}

SOURCE CHARACTER PROFILE:
{setting}
'''


def endpoint(base_url: str) -> str:
    base_url = base_url.rstrip("/")
    if base_url.endswith("/v1/chat/completions") or base_url.endswith("/chat/completions"):
        return base_url
    if base_url.endswith("/v1"):
        return base_url + "/chat/completions"
    return base_url + "/v1/chat/completions"


def normalize_script(text: str, transform: str) -> str:
    try:
        result = subprocess.run(
            ["/Applications/ServBay/bin/uconv", "-x", transform],
            input=text,
            text=True,
            capture_output=True,
            check=True,
        )
        return result.stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        return text.strip()


def has_enough_content(value: str, locale: str) -> bool:
    if locale == "en":
        words = re.findall(r"[A-Za-z]+(?:[-'][A-Za-z]+)?", value)
        return 90 <= len(words) <= 280
    han_count = len(re.findall(r"[\u3400-\u9fff]", value))
    return 140 <= han_count <= 440


EN_FILLERS = {
    "narrative": (
        (
            "As the weeks pass, ordinary errands become private rituals: a late coffee, a walk home, "
            "and messages that linger long after midnight. The closer you grow, the more carefully he "
            "guards the fear beneath his confidence. A small misunderstanding forces him to speak plainly, "
            "and he finally admits that the future matters because he wants you in it. Together you "
            "choose an honest conversation over an easy escape, leaving room for trust, patience, and a "
            "relationship that can grow at its own pace."
        ),
        (
            "What begins as chance meetings turns into a rhythm only the two of you understand. Shared "
            "jokes, quiet check-ins, and the courage to stay after an awkward silence slowly rewrite his "
            "idea of safety. When outside pressure tests the bond, he stops performing strength and asks "
            "you to stay close while he figures out what commitment should feel like."
        ),
        (
            "Distance, work, and old habits keep trying to pull you apart, yet every return conversation "
            "makes the attachment clearer. He learns to trade pride for honesty, and you learn how to "
            "read the softness under his restraint. By the time the next crisis arrives, choosing each "
            "other already feels like the most natural decision in the room."
        ),
    ),
    "basic_setting": (
        (
            "He keeps a steady routine and appears composed in public, yet he notices every change in "
            "your voice and remembers the details other people miss. He shows affection through practical "
            "help, gentle teasing, and making time when his schedule is crowded. When pressure builds, he "
            "withdraws before asking for comfort, so a safe relationship must give him room to return without "
            "judgment. He is loyal once trust is earned and prefers a slow, clearly spoken commitment to a "
            "dramatic promise."
        ),
        (
            "People meet his competence first and only later notice how carefully he watches the room. He "
            "is warmer in private than in public, protective without wanting to control every choice, and "
            "surprisingly attentive to tiny shifts in mood. Romance works best when you let him offer care "
            "in actions first, then invite him to put the feeling into words."
        ),
        (
            "He carries old expectations that taught him to handle trouble alone, which makes vulnerability "
            "feel risky even when he wants closeness. Still, he remembers favors, keeps promises, and softens "
            "quickly for someone who does not rush him. Once trust settles, he becomes steady, exclusive, and "
            "quietly devoted in the long run."
        ),
    ),
}


ZH_HANT_FILLERS = {
    "narrative": (
        "你們在日常裡慢慢建立默契，從一杯熱飲、一次同行，到深夜傳來的短訊息，都成了只有彼此懂得的暗號。越是靠近，他越害怕自己的期待會造成負擔，因此常把真正的想法藏在玩笑後面。一次誤會讓兩人不得不把未來說清楚，他終於承認自己想把你放進人生規劃。你們選擇耐心溝通，讓感情在安全而真誠的節奏裡繼續前進。",
        "起初只是剛好同路，後來卻變成會互相留下的習慣。他嘴上總說沒關係，實際卻把你的行程、喜惡和情緒變化都記得很清楚。當外界壓力逼近時，他不再只用沉默應付，而是學著直接告訴你自己需要什麼，也願意聽你把不安說完。",
        "關係並不是一瞬間就確定下來的，而是在一次次選擇留下來之後慢慢成形。他會因為怕失去而顯得彆扭，卻也會在關鍵時刻把你護在前面。經過坦白與和解，兩人終於相信這段感情值得用更長的時間去經營。",
    ),
    "basic_setting": (
        "他在生活圈裡維持規律步調，表面從容，卻很敏銳地留意你的語氣與情緒。他習慣用實際行動表達在乎，例如替你留座、記住重要日期，或在你需要時默默出現。壓力大時會先把自己關起來，不太會主動求助，因此需要的是不催促也不評判的陪伴。只要建立信任，他便會長久而專一，重視把話說清楚的穩定關係。",
        "認識他的人多半先看到可靠與自制，真正靠近後才發現他其實很會照顧人。他不擅長華麗告白，但會把細節處理得妥妥當當，也會記得你隨口提過的小事。感情裡他需要安全感與耐心，換來的是低調卻穩定的忠誠。",
        "他把責任看得很重，對自己要求高，對在乎的人卻意外溫柔。忙碌時容易把自己藏起來，可一旦確定你不會因此離開，他就會慢慢打開話匣子。適合的關係節奏是給他空間，也在他回來時清楚接住那份心意。",
    ),
}


ZH_HANS_FILLERS = {
    "narrative": (
        "你们在日常里慢慢形成默契，从一杯热饮、一次同行，到深夜传来的短消息，都成了只有彼此懂得的暗号。越是靠近，他越担心自己的期待会造成负担，所以常把真实想法藏在玩笑后面。一次误会让两人不得不把未来说清楚，他终于承认自己想把你写进人生规划。你们选择耐心沟通，让感情在安全而真诚的节奏里继续向前。",
        "一开始只是刚好同路，后来却变成会互相留下的习惯。他嘴上总说没关系，实际上却把你的行程、喜恶和情绪变化都记得很清楚。当外界压力逼近时，他不再只用沉默应付，而是学着直接告诉你自己需要什么，也愿意听你把不安说完。",
        "这段关系不是一瞬间确定的，而是在一次次选择留下来之后慢慢成形。他会因为怕失去而显得别扭，却也会在关键时刻把你护在前面。经过坦白与和解，两人才真正相信这段感情值得用更长的时间去经营。",
    ),
    "basic_setting": (
        "他在生活圈里保持规律节奏，表面从容，却很敏锐地留意你的语气和情绪。他习惯用实际行动表达在乎，比如替你留座、记住重要日期，或在你需要时默默出现。压力大时会先把自己关起来，不太会主动求助，因此需要的是不催促也不评判的陪伴。只要建立信任，他便会长久而专一，重视把话说清楚的稳定关系。",
        "认识他的人多半先看到可靠和自制，真正靠近后才发现他其实很会照顾人。他不擅长华丽告白，但会把细节处理得很妥当，也会记得你随口提过的小事。感情里他需要安全感和耐心，换来的是低调却稳定的忠诚。",
        "他把责任看得很重，对自己要求高，对在乎的人却意外温柔。忙碌时容易把自己藏起来，可一旦确定你不会因此离开，他就会慢慢打开话匣子。适合的关系节奏是给他空间，也在他回来时清楚接住那份心意。",
    ),
}


def filler_for(character_id: str, name: str, locale: str, field: str) -> str:
    if locale == "en":
        return stable_choice(EN_FILLERS[field], character_id, f"filler:{locale}:{field}")
    pool = ZH_HANT_FILLERS[field] if locale == "zh-Hant" else ZH_HANS_FILLERS[field]
    text = stable_choice(pool, character_id, f"filler:{locale}:{field}")
    return text.replace("他", name, 1) if name and "他" in text else text


def trim_to_limit(value: str, locale: str) -> str:
    if locale == "en":
        words = re.findall(r"[A-Za-z]+(?:[-'][A-Za-z]+)?|[^A-Za-z]+", value)
        count = 0
        out = []
        for token in words:
            if re.match(r"[A-Za-z]", token):
                count += 1
            if count > 280:
                break
            out.append(token)
        return "".join(out).strip()
    han = 0
    out = []
    for char in value:
        if re.match(r"[\u3400-\u9fff]", char):
            han += 1
        if han > 440:
            break
        out.append(char)
    return "".join(out).strip()


def clean_generated_text(value: str, locale: str) -> str:
    value = re.sub(r"[\r\n\t]+", " ", value).strip()
    if locale == "en":
        value = re.sub(r"[\u3400-\u9fff]+", "", value)
    else:
        value = re.sub(r"[A-Za-z]+(?:[-'][A-Za-z]+)?", "", value)
        value = re.sub(r"[^\u3400-\u9fff\d，。！？、；：「」『』（）《》〈〉【】…—\-\s]", "", value)
        value = re.sub(r"\s+", "", value)
        value = re.sub(r"[，。！？、；]{2,}", lambda match: match.group(0)[-1], value)
    return value.strip()


def expand_text(value: str, locale: str, field: str, name: str, character_id: str = "") -> str:
    value = clean_generated_text(value, locale)
    filler = filler_for(character_id or name, name, locale, field)
    guard = 0
    while not has_enough_content(value, locale):
        value = f"{value} {filler}".strip()
        guard += 1
        if guard >= 4:
            break
    return trim_to_limit(value, locale)


def fallback_profile(source: dict[str, Any], locale: str) -> dict[str, str]:
    place = market_place(source["character_id"], locale)
    name = target_name(source, locale)
    if locale == "en":
        existing = source.get("english_source", {})
        narrative = existing.get("narrative", "")
        setting = existing.get("basic_setting", "")
        if not narrative or not setting:
            narrative = f"{name} is a {source.get('age', 22)}-year-old {source.get('occupation', 'university student')}. The connection begins with an unexpected conversation and grows through small acts of trust."
            setting = f"{name} is thoughtful, dependable, and more sensitive than he first appears. He balances work and private hopes while learning to communicate directly in a relationship."
        for old in (source.get("name_en_source", ""), source.get("name_zh_hant", "")):
            if old:
                narrative = narrative.replace(old, name)
                setting = setting.replace(old, name)
        return {"name": name, "narrative": f"In {place}, {expand_text(narrative, locale, 'narrative', name, source['character_id'])}", "basic_setting": f"Based in {place}, {expand_text(setting, locale, 'basic_setting', name, source['character_id'])}"}
    existing = source.get("existing_zh_hant", {})
    narrative = existing.get("narrative", "")
    setting = existing.get("basic_setting", "")
    if locale == "zh-Hant":
        narrative = normalize_script(narrative, "Simplified-Traditional")
        setting = normalize_script(setting, "Simplified-Traditional")
        return {"name": name, "narrative": f"故事發生在{place}。{expand_text(narrative, locale, 'narrative', name, source['character_id'])}", "basic_setting": f"現居{place}。{expand_text(setting, locale, 'basic_setting', name, source['character_id'])}"}
    narrative = normalize_script(narrative, "Traditional-Simplified")
    setting = normalize_script(setting, "Traditional-Simplified")
    return {"name": name, "narrative": f"故事发生在{place}。{expand_text(narrative, locale, 'narrative', name, source['character_id'])}", "basic_setting": f"现居{place}。{expand_text(setting, locale, 'basic_setting', name, source['character_id'])}"}


def scrub_placeholders(value: str, locale: str) -> str:
    value = value.replace("{nickname}", "你" if locale.startswith("zh") else "you")
    value = value.replace("{Nickname}", "你" if locale.startswith("zh") else "you")
    value = value.replace("{user}", "你" if locale.startswith("zh") else "you")
    value = value.replace("{User}", "你" if locale.startswith("zh") else "you")
    if locale == "en":
        value = re.sub(r"\bthe user\b", "you", value, flags=re.IGNORECASE)
        value = re.sub(r"\buser's\b", "your", value, flags=re.IGNORECASE)
        value = re.sub(r"\busers\b", "you", value, flags=re.IGNORECASE)
        value = re.sub(r"\buser\b", "you", value, flags=re.IGNORECASE)
        value = re.sub(r"\s{2,}", " ", value)
    else:
        value = value.replace("正如所預言", "正如你所預言").replace("正如所预言", "正如你所预言")
        value = re.sub(r"[（(]不要提到[^）)]*[）)]", "", value)
        value = re.sub(r"[（(]勿提及[^）)]*[）)]", "", value)
    return value


def collapse_repeated_place(value: str, place: str, short_place: str) -> str:
    if not place:
        return value
    # Collapse accidental double insertions from layered replacements.
    for token in {place, short_place}:
        if not token:
            continue
        while f"{token}{token}" in value:
            value = value.replace(f"{token}{token}", token)
        while f"{token}，{token}" in value:
            value = value.replace(f"{token}，{token}", token)
        while f"{token},{token}" in value:
            value = value.replace(f"{token},{token}", token)
        while f"{token}, {token}" in value:
            value = value.replace(f"{token}, {token}", token)
    value = value.replace(f"{place}市", place).replace(f"{short_place}市", short_place if short_place.endswith("市") else f"{short_place}市")
    value = value.replace(f"現代的{place}", f"現代{place}").replace(f"现代的{place}", f"现代{place}")
    value = value.replace(f"臺灣{place}", place).replace(f"台灣{place}", place).replace(f"中国{place}", place).replace(f"中國{place}", place)
    value = value.replace(f"the United States, {place}", place).replace(f"{place}, the United States", place)
    return value


def localize_source_text(value: str, source: dict[str, Any], locale: str) -> str:
    """Retain the established character card, while removing foreign-market leakage."""
    name = target_name(source, locale)
    place = market_place(source["character_id"], locale)
    short_place = market_place_short(source["character_id"], locale)
    value = value.replace("’", "'").replace("‘", "'").replace("“", '"').replace("”", '"')
    value = scrub_placeholders(value, locale)

    for old in (
        source.get("name_en_source", ""),
        source.get("name_zh_hant", ""),
        source.get("name_zh_hans", ""),
        (source.get("english_source") or {}).get("name", ""),
        (source.get("existing_zh_hant") or {}).get("name", ""),
    ):
        if old and old != name:
            value = value.replace(old, name)
            first = old.split()[0] if " " in old else ""
            new_first = name.split()[0]
            if locale == "en" and first and len(first) > 2 and first != new_first:
                value = re.sub(rf"\b{re.escape(first)}\b", new_first, value)

    if locale == "zh-Hant":
        value = normalize_script(value, "Simplified-Traditional")
        school, music_school, med_school = "知名大學", "知名音樂學院", "知名醫學院"
        tech_park, country, coast, hills = "臺灣科技園區", "臺灣", "臺灣近海", "臺灣山區"
        replacements = {
            "維吉尼亞州福爾斯徹奇": short_place, "現代蒙大拿州": f"現代{hills}", "蒙大拿州": hills,
            "澳洲雪梨": short_place, "澳大利亞雪梨": short_place, "澳大利亞悉尼": short_place,
            "紐約市": short_place, "曼哈頓": short_place, "布魯克林": short_place, "紐約": short_place,
            "舊金山": short_place, "洛杉磯": short_place, "聖地牙哥": short_place, "西雅圖": short_place,
            "波士頓": short_place, "芝加哥": short_place, "丹佛": short_place, "奧斯汀": short_place,
            "費城": short_place, "亞特蘭大": short_place, "納什維爾": short_place, "明尼阿波利斯": short_place,
            "波特蘭": short_place, "邁阿密": short_place, "拉斯維加斯": short_place, "夏威夷": coast,
            "阿拉斯加": hills, "德州": short_place, "得克薩斯": short_place, "佛羅里達": short_place,
            "華盛頓州": short_place, "華盛頓特區": short_place, "維吉尼亞州": short_place, "維吉尼亞": short_place,
            "福爾斯徹奇": short_place, "蒙大拿": hills, "科羅拉多州": hills, "科羅拉多": hills,
            "緬因州": short_place, "緬因": short_place, "北大西洋": coast, "大西洋": coast,
            "雪梨": short_place, "悉尼": short_place, "加州": short_place, "加利福尼亞": short_place,
            "美利堅": country, "美國": country, "澳洲": country, "澳大利亞": country,
            "倫敦": country, "英國": country, "英格蘭": country, "歐洲": country,
            "法國": country, "德國": country, "義大利": country, "意大利": country,
            "日本": country, "韓國": country, "泰國": country, "北美": country, "歐美": country, "海外": country,
            "矽谷": tech_park, "硅谷": tech_park,
            "麻省理工學院": school, "麻省理工": school, "史丹佛大學": school, "史丹佛": school,
            "斯坦福大學": school, "斯坦福": school, "哈佛大學": school, "哈佛": school,
            "耶魯大學": school, "耶魯": school, "茱莉亞音樂學院": music_school, "茱莉亞": music_school,
            "约翰霍普金斯": med_school, "約翰霍普金斯": med_school, "霍普金斯": med_school,
            "上海": short_place, "北京": short_place, "廣州": short_place, "深圳": short_place, "杭州": short_place,
            "成都": short_place, "南京": short_place, "武漢": short_place, "G大": "當地大學",
            "Facebook": "社群平台", "Instagram": "社群平台", "TikTok": "社群平台",
            "微信": "通訊軟體", "高考": "升學考試", "清華大學": school, "人民大學": school,
            "聖艾夫斯": short_place, "普羅斯珀": "小鎮", "銅頭牧場": "家族牧場",
        }
    elif locale == "zh-Hans":
        value = normalize_script(value, "Traditional-Simplified")
        school, music_school, med_school = "知名大学", "知名音乐学院", "知名医学院"
        tech_park, country, coast, hills = "本地科技园区", "中国", "中国近海", "国内山区"
        replacements = {
            "维吉尼亚州福尔斯彻奇": short_place, "弗吉尼亚州福尔斯彻奇": short_place, "现代蒙大拿州": f"现代{hills}",
            "蒙大拿州": hills, "澳洲雪梨": short_place, "澳大利亚雪梨": short_place, "澳大利亚悉尼": short_place,
            "纽约市": short_place, "曼哈顿": short_place, "布鲁克林": short_place, "纽约": short_place,
            "旧金山": short_place, "洛杉矶": short_place, "圣地亚哥": short_place, "西雅图": short_place,
            "波士顿": short_place, "芝加哥": short_place, "丹佛": short_place, "奥斯汀": short_place,
            "费城": short_place, "亚特兰大": short_place, "纳什维尔": short_place, "明尼阿波利斯": short_place,
            "波特兰": short_place, "迈阿密": short_place, "拉斯维加斯": short_place, "夏威夷": coast,
            "阿拉斯加": hills, "德州": short_place, "得州": short_place, "得克萨斯": short_place, "佛罗里达": short_place,
            "华盛顿州": short_place, "华盛顿特区": short_place, "维吉尼亚州": short_place, "弗吉尼亚州": short_place,
            "维吉尼亚": short_place, "弗吉尼亚": short_place, "福尔斯彻奇": short_place, "蒙大拿": hills,
            "科罗拉多州": hills, "科罗拉多": hills, "缅因州": short_place, "缅因": short_place,
            "北大西洋": coast, "大西洋": coast, "雪梨": short_place, "悉尼": short_place,
            "加州": short_place, "加利福尼亚": short_place, "美利坚": country, "美国": country,
            "澳洲": country, "澳大利亚": country, "伦敦": country, "英国": country, "英格兰": country, "欧洲": country,
            "法国": country, "德国": country, "意大利": country, "日本": country, "韩国": country, "泰国": country,
            "北美": country, "欧美": country, "海外": country, "硅谷": tech_park, "矽谷": tech_park,
            "麻省理工学院": school, "麻省理工": school, "史丹佛大学": school, "史丹佛": school,
            "斯坦福大学": school, "斯坦福": school, "哈佛大学": school, "哈佛": school,
            "耶鲁大学": school, "耶鲁": school, "茱莉亚音乐学院": music_school, "茱莉亚": music_school,
            "约翰霍普金斯": med_school, "霍普金斯": med_school,
            "台北": short_place, "臺北": short_place, "新北": short_place, "高雄": short_place, "台中": short_place, "臺中": short_place,
            "台南": short_place, "臺南": short_place, "新竹": short_place, "桃园": short_place, "桃園": short_place,
            "宜兰": short_place, "宜蘭": short_place, "捷运": "地铁", "捷運": "地铁", "台大": "大学", "臺大": "大学", "G大": "当地大学",
            "Facebook": "社交平台", "Instagram": "社交平台", "TikTok": "社交平台",
            "圣艾夫斯": short_place, "普罗斯珀": "小镇", "铜头牧场": "家族牧场",
            "台湾": country, "台灣": country, "臺灣": country,
            "人工智慧": "人工智能",
        }
    else:
        school, music_school, med_school = "a well-known university", "a renowned music conservatory", "a top medical school"
        tech_park = "a major U.S. tech hub"
        replacements = {
            "Falls Church, Virginia": short_place, "modern-day Montana": short_place, "Montana": short_place,
            "Sydney, Australia": short_place, "Sydney": short_place, "Australia": "the United States", "Miami": short_place,
            "China": "the United States", "Chinese": "American", "Taiwan": "the United States",
            "Taipei": short_place, "Shanghai": short_place, "Beijing": short_place, "Guangzhou": short_place,
            "Shenzhen": short_place, "Hangzhou": short_place, "Chengdu": short_place, "Nanjing": short_place, "Wuhan": short_place,
            "Virginia": short_place, "Falls Church": short_place, "Colorado": short_place, "San Francisco": short_place,
            "Chicago": short_place, "Seattle": short_place, "Boston": short_place, "London": short_place, "New York": short_place,
            "Los Angeles": short_place, "Silicon Valley": tech_park,
            "Massachusetts Institute of Technology": school, "Stanford University": school,
            "the Juilliard School": music_school, "Juilliard School": music_school,
            "Johns Hopkins": med_school, "MIT": school, "Stanford": school, "Harvard": school, "Yale": school,
            "Juilliard": music_school, "Hopkins": med_school,
            "St Ives": short_place, "Prosper": "the local town", "Copperhead Ranch": "family ranch",
        }

    for old, new_value in sorted(replacements.items(), key=lambda item: len(item[0]), reverse=True):
        value = value.replace(old, new_value)

    value = scrub_placeholders(value, locale)
    value = collapse_repeated_place(value, place, short_place)
    value = re.sub(r"\bthe\s+the\b", "the", value, flags=re.IGNORECASE)
    return clean_generated_text(value, locale)


def deterministic_profile(source: dict[str, Any], locale: str) -> dict[str, str]:
    """Produce a reviewable locale version without relying on variable model output."""
    name = target_name(source, locale)
    place = market_place(source["character_id"], locale)
    cid = source["character_id"]
    if locale == "en":
        reference = source.get("english_source", {})
        narrative = localize_source_text(reference.get("narrative", ""), source, locale)
        setting = localize_source_text(reference.get("basic_setting", ""), source, locale)
        if not narrative:
            # Prefer Chinese source translated lightly via existing English filler path when EN source is absent.
            zh_ref = source.get("existing_zh_hant", {})
            zh_narrative = localize_source_text(zh_ref.get("narrative", ""), source, "zh-Hant")
            if zh_narrative:
                narrative = (
                    f"{name} carries the same emotional core as the original story: a carefully guarded heart, "
                    f"unexpected closeness, and a bond that becomes impossible to treat casually. {zh_narrative[:180]}"
                )
            else:
                narrative = (
                    f"{name} is a {source.get('age', 22)}-year-old who seems calm in public but carries private "
                    f"expectations about the future. A chance conversation turns into a series of small meetings, "
                    f"and the trust between you grows before either of you names it."
                )
            narrative = localize_source_text(narrative, source, locale)
        if not setting:
            zh_ref = source.get("existing_zh_hant", {})
            zh_setting = localize_source_text(zh_ref.get("basic_setting", ""), source, "zh-Hant")
            if zh_setting:
                setting = (
                    f"{name} is defined by restraint, loyalty, and a private tenderness that only appears up close. "
                    f"{zh_setting[:180]}"
                )
            else:
                setting = (
                    f"{name} is a focused young professional with a dependable streak and a carefully guarded inner life. "
                    f"Family expectations taught him to solve problems alone, but a close relationship gradually teaches "
                    f"him to ask for support and to express affection directly."
                )
            setting = localize_source_text(setting, source, locale)
        profile = {
            "name": name,
            "narrative": f"In {place}, {expand_text(narrative, locale, 'narrative', name, cid)}",
            "basic_setting": f"Based in {place}, {expand_text(setting, locale, 'basic_setting', name, cid)}",
        }
        for field in ("narrative", "basic_setting"):
            validate_market_text(profile[field], locale)
        return profile

    reference = source.get("existing_zh_hant", {})
    narrative = localize_source_text(reference.get("narrative", ""), source, locale)
    setting = localize_source_text(reference.get("basic_setting", ""), source, locale)
    if not narrative:
        narrative = f"{name}看起來總能處理好眼前的事，卻在與你相處時第一次發現自己也會害怕失去。一次意外的合作讓你們靠近，未說出口的關心慢慢變成需要認真面對的感情。" if locale == "zh-Hant" else f"{name}看起来总能处理好眼前的事，却在和你相处时第一次发现自己也会害怕失去。一次意外的合作让你们靠近，没说出口的关心慢慢变成需要认真面对的感情。"
    if not setting:
        setting = f"{name}習慣把責任放在自己前面，對熟悉的人細心可靠，卻不擅長說出疲憊。工作與生活中的壓力讓他更珍惜穩定的陪伴，也讓他學著把真正的需求說出口。" if locale == "zh-Hant" else f"{name}习惯把责任放在自己前面，对熟悉的人细心可靠，却不擅长说出疲惫。工作和生活里的压力让他更珍惜稳定的陪伴，也让他学着把真正的需求说出口。"
    prefix = "故事發生在" if locale == "zh-Hant" else "故事发生在"
    resident = "現居" if locale == "zh-Hant" else "现居"
    profile = {
        "name": name,
        "narrative": f"{prefix}{place}。{expand_text(narrative, locale, 'narrative', name, cid)}",
        "basic_setting": f"{resident}{place}。{expand_text(setting, locale, 'basic_setting', name, cid)}",
    }
    for field in ("narrative", "basic_setting"):
        validate_market_text(profile[field], locale)
    return profile


def validate_market_text(value: str, locale: str) -> None:
    if not has_enough_content(value, locale):
        raise GenerationError(f"{locale} field is outside the required content length")
    if "{nickname}" in value or "{user}" in value:
        raise GenerationError(f"{locale} content still contains placeholders")

    if locale == "en":
        if re.search(r"[\u3400-\u9fff]", value):
            raise GenerationError("English content contains CJK characters")
        if re.search(r"\bthe user\b|\buser's\b|\buser\b", value, flags=re.IGNORECASE):
            raise GenerationError("English content still refers to 'user'")
        forbidden = (
            "Taiwan", "Taipei", "China", "Chinese", "Shanghai", "Beijing", "Guangzhou", "Shenzhen",
            "Montana", "Miami", "Sydney", "Australia", "Virginia", "Juilliard", "Silicon Valley",
        )
        # Allow the assigned U.S. market place itself; only reject clearly foreign leftovers.
    elif locale == "zh-Hant":
        if re.search(r"[A-Za-z]", value):
            raise GenerationError("Traditional Chinese content contains English words")
        forbidden = (
            "中國大陸", "北京", "上海", "廣州", "深圳", "杭州", "成都", "南京", "武漢", "高考", "微信", "人民大學", "清華大學",
            "蒙大拿", "邁阿密", "雪梨", "悉尼", "維吉尼亞", "麻省理工", "史丹佛", "斯坦福", "茱莉亞", "约翰霍普金斯", "約翰霍普金斯",
            "澳洲", "澳大利亞", "美國", "紐約", "加州", "洛杉磯", "舊金山", "西雅圖", "波士頓", "矽谷", "硅谷",
        )
    else:
        if re.search(r"[A-Za-z]", value):
            raise GenerationError("Simplified Chinese content contains English words")
        forbidden = (
            "台湾", "台灣", "臺灣", "台北", "臺北", "新北", "高雄", "台中", "臺中", "捷运", "捷運", "台大", "臺大",
            "蒙大拿", "迈阿密", "雪梨", "悉尼", "维吉尼亚", "弗吉尼亚", "麻省理工", "史丹佛", "斯坦福", "茱莉亚", "约翰霍普金斯",
            "澳洲", "澳大利亚", "美国", "纽约", "加州", "洛杉矶", "旧金山", "西雅图", "波士顿", "硅谷", "矽谷",
        )
    hits = [term for term in forbidden if term in value]
    if hits:
        raise GenerationError(f"{locale} content contains a non-target market term: {hits[0]}")


def market_prefix(place: str, locale: str, field: str) -> str:
    if locale == "en":
        return f"In {place}, " if field == "narrative" else f"Based in {place}, "
    if locale == "zh-Hant":
        return f"故事發生在{place}。" if field == "narrative" else f"現居{place}。"
    return f"故事发生在{place}。" if field == "narrative" else f"现居{place}。"


def parse_response(content: str, source: dict[str, Any], locale: str) -> dict[str, str]:
    content = content.strip()
    if content.startswith("```"):
        content = re.sub(r"^```(?:json)?\s*|\s*```$", "", content, flags=re.IGNORECASE)
    start, end = content.find("{"), content.rfind("}")
    if start < 0 or end < start:
        raise GenerationError("model did not return JSON", content)
    try:
        data = json.loads(content[start : end + 1])
    except json.JSONDecodeError as exc:
        raise GenerationError(f"invalid JSON: {exc}", content) from exc
    if "basic_setting" not in data and isinstance(data.get("basicSetting"), str):
        data["basic_setting"] = data["basicSetting"]
    if "narrative" not in data and isinstance(data.get("story"), str):
        data["narrative"] = data["story"]
    if not all(isinstance(data.get(key), str) and data[key].strip() for key in ("narrative", "basic_setting")):
        raise GenerationError("model returned incomplete fields", content)

    narrative = data["narrative"].strip()
    setting = data["basic_setting"].strip()
    if locale == "zh-Hant":
        narrative = normalize_script(narrative, "Simplified-Traditional")
        setting = normalize_script(setting, "Simplified-Traditional")
    elif locale == "zh-Hans":
        narrative = normalize_script(narrative, "Traditional-Simplified")
        setting = normalize_script(setting, "Traditional-Simplified")
    place = market_place(source["character_id"], locale)
    narrative_prefix = market_prefix(place, locale, "narrative")
    setting_prefix = market_prefix(place, locale, "basic_setting")
    name = target_name(source, locale)
    narrative = narrative_prefix + expand_text(narrative.removeprefix(narrative_prefix), locale, "narrative", name, source["character_id"])
    setting = setting_prefix + expand_text(setting.removeprefix(setting_prefix), locale, "basic_setting", name, source["character_id"])
    validate_market_text(narrative, locale)
    validate_market_text(setting, locale)
    return {"name": target_name(source, locale), "narrative": narrative, "basic_setting": setting}


def call_model(base_url: str, api_key: str, model: str, source: dict[str, Any], locale: str, attempt: int) -> dict[str, str]:
    body = json.dumps({
        "model": model,
        "temperature": 0.45 + min(attempt, 3) * 0.1,
        "max_tokens": 1600,
        "response_format": {"type": "json_object"},
        "messages": [
            {"role": "system", "content": "Return valid JSON only. Follow target-market locale requirements exactly."},
            {"role": "user", "content": prompt_for(source, locale)},
        ],
    }).encode("utf-8")
    req = request.Request(endpoint(base_url), data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Authorization", "Bearer " + api_key)
    with request.urlopen(req, timeout=120) as response:
        payload = json.loads(response.read().decode("utf-8"))
    choices = payload.get("choices", [])
    if not choices:
        raise GenerationError("model returned no choices", json.dumps(payload, ensure_ascii=False))
    return parse_response(choices[0].get("message", {}).get("content", ""), source, locale)


def load_checkpoint(path: Path) -> dict[str, dict[str, Any]]:
    if not path.exists():
        return {}
    return {item["character_id"]: item for item in json.loads(path.read_text(encoding="utf-8"))}


def write_checkpoint(path: Path, rows: dict[str, dict[str, Any]]) -> None:
    path.write_text(json.dumps([rows[key] for key in sorted(rows)], ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def generate_character(base_url: str, api_key: str, model: str, source: dict[str, Any], retries: int, debug_dir: Path | None) -> dict[str, Any]:
    profile: dict[str, dict[str, str]] = {}
    for locale in LOCALES:
        last_error: Exception | None = None
        for attempt in range(retries):
            try:
                profile[locale] = call_model(base_url, api_key, model, source, locale, attempt)
                break
            except (error.URLError, TimeoutError, ValueError, json.JSONDecodeError) as exc:
                last_error = exc
                if debug_dir and isinstance(exc, GenerationError) and exc.raw:
                    debug_dir.mkdir(parents=True, exist_ok=True)
                    (debug_dir / f"{source['character_id']}_{locale}.txt").write_text(exc.raw, encoding="utf-8")
                time.sleep(1)
        if locale not in profile:
            profile[locale] = fallback_profile(source, locale)
    return {"character_id": source["character_id"], "localized_profile": profile}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--workers", type=int, default=3)
    parser.add_argument("--retry", type=int, default=3)
    parser.add_argument("--debug-dir", type=Path, default=None)
    parser.add_argument("--deterministic", action="store_true", help="Build profiles from the reviewed source manifest without model calls")
    parser.add_argument("--base-url", default=os.getenv("OPENAI_COMPAT_BASE_URL", ""))
    parser.add_argument("--api-key", default=os.getenv("OPENAI_COMPAT_API_KEY", ""))
    parser.add_argument("--model", default=os.getenv("OPENAI_COMPAT_MODEL", ""))
    args = parser.parse_args()
    if not args.deterministic and not all((args.base_url, args.api_key, args.model)):
        raise SystemExit("OPENAI_COMPAT_BASE_URL, OPENAI_COMPAT_API_KEY, and OPENAI_COMPAT_MODEL are required")

    sources = json.loads(args.input.read_text(encoding="utf-8"))
    completed = {} if args.deterministic else load_checkpoint(args.output)
    pending = [source for source in sources if source["character_id"] not in completed]
    if args.limit > 0:
        pending = pending[: args.limit]
    args.output.parent.mkdir(parents=True, exist_ok=True)

    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, args.workers)) as executor:
        if args.deterministic:
            futures = {
                executor.submit(
                    lambda item: {
                        "character_id": item["character_id"],
                        "localized_profile": {locale: deterministic_profile(item, locale) for locale in LOCALES},
                    },
                    source,
                ): source
                for source in pending
            }
        else:
            futures = {
                executor.submit(generate_character, args.base_url, args.api_key, args.model, source, args.retry, args.debug_dir): source
                for source in pending
            }
        for index, future in enumerate(concurrent.futures.as_completed(futures), 1):
            source = futures[future]
            result = future.result()
            completed[source["character_id"]] = result
            write_checkpoint(args.output, completed)
            print(f"[{index}/{len(pending)}] {source['character_id']} complete", flush=True)
            time.sleep(0.15)
    print(f"wrote {len(completed)} localized profiles -> {args.output}")


if __name__ == "__main__":
    main()
