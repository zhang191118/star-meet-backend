#!/usr/bin/env python3
"""Generate 4K banner and chat-background images for Crushie public characters.

Uses the OpenAI-compatible image edits API so each character's existing avatar
can lock face identity. One request can only return one size, so banner and
background are generated as two separate calls.

Environment:
  AI98PRO_API_KEY   required
  AI98PRO_BASE_URL  default https://ai98pro.xyz
"""
from __future__ import annotations

import argparse
import base64
import json
import os
import sys
import time
from io import BytesIO
from pathlib import Path

import requests
from PIL import Image

DEFAULT_BASE = "https://ai98pro.xyz"
DEFAULT_MODEL = "gpt-image-2"
BANNER_SIZE = "3840x2160"
BG_SIZE = "2160x3840"

# Production public characters that already have banner_image. Only generate bg.
HAS_PROD_BANNER = {
    "e8ab59af",  # 傅景行
    "a9d39634",  # 江以澈
    "d9da2ab7",  # 沈硯之
    "1d88f672",  # 溫紹謙
    "96d2457b",  # 程晏州
    "9b408ed1",  # 簡予安
    "0fab69ca",  # 聶遠舟
    "56e80ffb",  # 陸子謙
    "aaa1c082",  # 顧承澤
    "da9b5cfe",  # 黎慕深
}

STYLE_LOCK = (
    "Photorealistic cinematic photography, real human skin with pores and fine texture, "
    "85mm lens, shallow depth of field, natural movie lighting, high-end still from a "
    "live-action film. Keep the exact same person as the reference photo: same face, "
    "same facial structure, same hairstyle, same hair color, same skin tone, same ethnicity. "
    "Portray him as an adult man in his mid-20s, clearly over 21. Do not depict a teenager, "
    "schoolboy, or underage person. Do not change his identity, do not beautify into a different man, "
    "do not add a second person. No text, no letters, no watermark, no logo, no caption, "
    "no UI, no anime, no illustration, no cartoon, no CGI look. No bedroom, no bed, no alcohol, "
    "no restraint, no screaming, no school uniform, no classroom."
)

UNSAFE_SCENE_MARKERS = (
    "床", "醉", "按住", "禁錮", "禁锢", "尖叫", "手腕", "校園", "校园",
    "大學生", "大学生", "学生", "學生", "未成年", "drunk", "bed", "scream",
)
STUDENT_MARKERS = ("學生", "学生", "大學", "大学", "校園", "校园", "研究生")

# Safer occupation / scene replacements for known policy failures.
SAFE_OVERRIDES = {
    "83732c3b": {  # 霍景珩
        "age": 26,
        "occupation": "civil engineer",
        "basic_setting": "An adult civil engineer in his mid-20s with a sharp spatial mind, working on modern bridges and city infrastructure at dusk.",
        "first_situation": "Cinematic dusk construction site in a modern city, steel beams and crane silhouettes, an adult civil engineer holding blueprints, heroic and calm. No collapse panic, no school, no university.",
    },
    "699a90bc": {  # 容景行
        "age": 25,
        "occupation": "former competitive swimmer",
        "basic_setting": "An adult former competitive swimmer with an athletic build, now quieter after leaving the sport, often found by the water at night.",
        "first_situation": "Night lakeside dock, still water reflecting moonlight, an adult former swimmer sitting in a dark coat, melancholic cinematic mood. No campus, no school, no teenager.",
    },
    "3d3318b1": {  # 厲寒霜
        "age": 26,
        "occupation": "mysterious fox-spirit gentleman",
        "basic_setting": "A centuries-old fox spirit appearing as an adult man in his mid-20s, silver hair, gold eyes, protective and composed, walking a rainy modern city at night.",
        "first_situation": "Cinematic rainy night street under neon and lantern light, silver-haired adult man with a subtle fox-spirit aura, standing in the rain, elegant and still. No bedroom, no bed, no touching, no alcohol, no screaming.",
    },
    "4e7be8ff": {  # 霍言琛
        "age": 25,
        "occupation": "professional football player",
        "basic_setting": "An adult professional football player with a secret wild side, athletic, composed, standing under stadium floodlights after a night match.",
        "first_situation": "Night stadium floodlights after a match, athletic adult footballer in a dark sports jacket, cinematic sports-hero portrait. No school, no teenager.",
    },
    "8efe152f": {  # 林知遇——夜店场景两次生成均主体欠曝成剪影，强制人物打亮
        "age": 25,
        "occupation": "nightclub executive",
        "first_situation": "A powerful adult nightclub executive standing inside his luxurious private lounge, dramatic cinematic key light clearly illuminating his face, hair, and upper body, elegant dark interior with soft neon accents in the background, subject sharply lit and in focus, visible facial details, no silhouette, no pure black areas near the head.",
    },
}


def clip(text, n):
    text = " ".join((text or "").replace("\n", " ").split())
    if len(text) <= n:
        return text
    return text[: n - 1].rstrip() + "…"


def appearance_text(ch):
    for key in ("basic_setting", "background"):
        val = str(ch.get(key) or "").strip()
        if val:
            return clip(val, 420)
    return ""


def scene_text(ch):
    bits = []
    occ = str(ch.get("occupation") or "").strip()
    if occ:
        bits.append("His role is " + occ + ".")
    sit = str(ch.get("first_situation") or "").strip()
    if sit:
        bits.append("Scene inspired by: " + clip(sit, 280))
    tags = ch.get("personality_tags") or ch.get("personality") or []
    if isinstance(tags, list) and tags:
        bits.append("Mood tags: " + ", ".join(str(t) for t in tags[:6]))
    return " ".join(bits)


def _as_int_age(value, default=25):
    try:
        return int(value)
    except (TypeError, ValueError):
        return default


def _rewrite_occupation(occupation):
    text = str(occupation or "").strip()
    if not text:
        return "young professional"
    if "土木" in text or "工程" in text:
        return "civil engineer"
    if "狐" in text:
        return "mysterious fox-spirit gentleman in a modern city"
    if "游泳" in text:
        return "former competitive swimmer"
    if "足球" in text:
        return "professional football player"
    if "籃球" in text or "篮球" in text:
        return "adult basketball team captain"
    if "油畫" in text or "油画" in text or "藝術" in text or "艺术" in text:
        return "professional painter"
    if "運動科學" in text or "运动科学" in text:
        return "sports scientist"
    if "研究生" in text:
        return "research professional"
    if any(marker in text for marker in STUDENT_MARKERS):
        return "young professional in a modern city"
    return text


def sanitize_character(ch):
    data = dict(ch)
    cid = str(data.get("character_id") or "")
    override = SAFE_OVERRIDES.get(cid) or {}
    data.update(override)
    age_n = _as_int_age(data.get("age"), 25)
    if age_n < 21:
        data["age"] = 25
        age_n = 25
    occupation = str(data.get("occupation") or "")
    if any(marker in occupation for marker in STUDENT_MARKERS) or cid in SAFE_OVERRIDES:
        data["occupation"] = override.get("occupation") or _rewrite_occupation(occupation)
    sit = str(data.get("first_situation") or "")
    if age_n < 21 or any(marker in sit for marker in UNSAFE_SCENE_MARKERS) or cid in SAFE_OVERRIDES:
        if "first_situation" in override:
            data["first_situation"] = override["first_situation"]
        else:
            data["first_situation"] = (
                "Cinematic adult portrait in an environment matching his occupation. "
                "Moody lighting, no school, no bedroom, no alcohol, no violence."
            )
    look = str(data.get("basic_setting") or data.get("background") or "")
    if cid in SAFE_OVERRIDES and "basic_setting" in override:
        data["basic_setting"] = override["basic_setting"]
        data["background"] = override["basic_setting"]
    elif any(marker in look for marker in ("醉", "床", "禁錮", "禁锢", "尖叫")):
        data["first_situation"] = data.get("first_situation") or (
            "Cinematic adult portrait in an environment matching his occupation. "
            "Moody lighting, no school, no bedroom, no alcohol, no violence."
        )
    return data


def build_prompt(ch, kind):
    ch = sanitize_character(ch)
    name = ch.get("name") or "the man"
    gender = "adult man" if str(ch.get("gender") or "male").lower().startswith("m") else "adult woman"
    look = appearance_text(ch)
    scene = scene_text(ch)
    identity = (
        "Create a new photograph of this " + gender + ", named " + str(name) + ", mid-20s, clearly over 21. "
        "He must remain unmistakably the same person as the attached reference portrait. "
        "Visual description: " + look
    )
    if kind == "banner":
        composition = (
            "Ultra-wide 16:9 cinematic hero still. Place the character in the right two-thirds "
            "of the frame, body visible at least from thighs up, gazing toward camera or slightly left. "
            "Keep the left third as soft empty negative space with blurred environment for a title overlay. "
            "Environment should match his occupation and the story scene. Premium dating-app recommend card."
        )
    else:
        composition = (
            "Vertical 9:16 atmospheric chat background. Full-height phone wallpaper. "
            "The character can appear, but keep the center and upper-middle relatively clear for message bubbles. "
            "Slightly darker, moody, cinematic, with rich environmental detail. "
            "Do not put a huge face in the exact center. Think of a premium chat backdrop."
        )
    return "\n".join([identity, scene, composition, STYLE_LOCK])


def decode_image_payload(payload):
    data = payload.get("data") or []
    if not data:
        raise RuntimeError("no image data in response: " + json.dumps(payload)[:500])
    item = data[0]
    if item.get("b64_json"):
        return base64.b64decode(item["b64_json"])
    url = item.get("url")
    if url:
        resp = requests.get(url, timeout=120)
        resp.raise_for_status()
        return resp.content
    raise RuntimeError("unsupported image payload: " + json.dumps(item)[:500])


def call_edits(session, base, key, model, avatar, prompt, size, quality, input_fidelity, timeout):
    url = base.rstrip("/") + "/v1/images/edits"
    headers = {"Authorization": "Bearer " + key}
    form = {
        "model": model,
        "prompt": prompt,
        "size": size,
        "quality": quality,
        "n": "1",
    }
    if input_fidelity:
        form["input_fidelity"] = input_fidelity
    with avatar.open("rb") as fh:
        files = {"image": (avatar.name, fh, "image/png")}
        resp = session.post(url, headers=headers, data=form, files=files, timeout=timeout)
    try:
        payload = resp.json()
    except Exception as exc:
        raise RuntimeError("HTTP %s non-json: %s" % (resp.status_code, resp.text[:800])) from exc
    if resp.status_code >= 400:
        raise RuntimeError("HTTP %s: %s" % (resp.status_code, json.dumps(payload, ensure_ascii=False)[:800]))
    return decode_image_payload(payload), payload


def call_generations(session, base, key, model, prompt, size, quality, timeout):
    url = base.rstrip("/") + "/v1/images/generations"
    headers = {"Authorization": "Bearer " + key, "Content-Type": "application/json"}
    body = {"model": model, "prompt": prompt, "size": size, "quality": quality, "n": 1}
    resp = session.post(url, headers=headers, json=body, timeout=timeout)
    try:
        payload = resp.json()
    except Exception as exc:
        raise RuntimeError("HTTP %s non-json: %s" % (resp.status_code, resp.text[:800])) from exc
    if resp.status_code >= 400:
        raise RuntimeError("HTTP %s: %s" % (resp.status_code, json.dumps(payload, ensure_ascii=False)[:800]))
    return decode_image_payload(payload), payload


def save_png(raw, dest, expected):
    dest.parent.mkdir(parents=True, exist_ok=True)
    image = Image.open(BytesIO(raw))
    if image.mode not in ("RGB", "RGBA"):
        image = image.convert("RGB")
    elif image.mode == "RGBA":
        bg = Image.new("RGB", image.size, (0, 0, 0))
        bg.paste(image, mask=image.split()[-1])
        image = bg
    image.save(dest, format="PNG", optimize=True)
    width, height = image.size
    info = {
        "path": str(dest),
        "bytes": dest.stat().st_size,
        "width": width,
        "height": height,
    }
    if expected and (width, height) != expected:
        info["size_warning"] = "got %sx%s, expected %sx%s" % (width, height, expected[0], expected[1])
    return info


def load_progress(path):
    if path.exists():
        return json.loads(path.read_text(encoding="utf-8"))
    return {}


def write_progress(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".tmp")
    tmp.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
    tmp.replace(path)


def expected_wh(size):
    w, h = size.split("x")
    return int(w), int(h)


def generate_one(session, args, ch, kind):
    cid = ch["character_id"]
    avatar = Path(ch.get("local_avatar") or "")
    if not avatar.exists():
        avatar = Path("/Users/guoliang/Documents/日常对话/crushie_data/avatars") / ("%s.png" % cid)
    if not avatar.exists():
        raise FileNotFoundError("missing avatar for %s" % cid)
    size = BANNER_SIZE if kind == "banner" else BG_SIZE
    dest = Path(args.out_dir) / ("%s_%s.png" % (cid, kind))
    prompt = build_prompt(ch, kind)
    last_error = None
    for attempt in range(1, args.retries + 1):
        t0 = time.time()
        try:
            if args.mode == "edits":
                raw, _payload = call_edits(
                    session, args.base_url, args.api_key, args.model, avatar,
                    prompt, size, args.quality, args.input_fidelity, args.timeout,
                )
            else:
                raw, _payload = call_generations(
                    session, args.base_url, args.api_key, args.model,
                    prompt, size, args.quality, args.timeout,
                )
            info = save_png(raw, dest, expected_wh(size))
            info.update({
                "status": "ok",
                "kind": kind,
                "attempt": attempt,
                "elapsed": round(time.time() - t0, 2),
                "mode": args.mode,
                "quality": args.quality,
                "size": size,
            })
            return info
        except Exception as exc:
            last_error = str(exc)
            print("[retry %s/%s] %s %s: %s" % (attempt, args.retries, cid, kind, exc), flush=True)
            time.sleep(min(8 * attempt, 30))
    return {
        "status": "failed",
        "kind": kind,
        "error": last_error,
        "size": size,
        "quality": args.quality,
        "mode": args.mode,
    }


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", type=Path, default=Path("/Users/guoliang/Documents/日常对话/crushie_data/crushie_80_translated.json"))
    parser.add_argument("--out-dir", type=Path, default=Path("/Users/guoliang/www/gocode/meihuoai/star-meet-backend/scripts/generated/crushie_4k"))
    parser.add_argument("--progress", type=Path, default=None)
    parser.add_argument("--only", action="append", default=[])
    parser.add_argument("--only-file", type=Path, default=None)
    parser.add_argument("--kinds", default="banner,bg")
    parser.add_argument("--quality", default="medium", choices=["low", "medium", "high", "auto"])
    parser.add_argument("--model", default=DEFAULT_MODEL)
    parser.add_argument("--mode", default="edits", choices=["edits", "generations"])
    parser.add_argument("--input-fidelity", default="high")
    parser.add_argument("--timeout", type=int, default=300)
    parser.add_argument("--retries", type=int, default=3)
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--force", action="store_true")
    parser.add_argument("--include-existing-banner", action="store_true")
    parser.add_argument("--sleep", type=float, default=1.0)
    parser.add_argument("--base-url", default=os.environ.get("AI98PRO_BASE_URL", DEFAULT_BASE))
    parser.add_argument("--api-key", default=os.environ.get("AI98PRO_API_KEY", ""))
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    if not args.api_key and not args.dry_run:
        raise SystemExit("AI98PRO_API_KEY is required")
    if args.progress is None:
        args.progress = args.out_dir / "progress.json"
    if args.input_fidelity in {"", "none", "null"}:
        args.input_fidelity = None
    return args


def main():
    args = parse_args()
    characters = json.loads(args.input.read_text(encoding="utf-8"))
    kinds = [k.strip() for k in args.kinds.split(",") if k.strip()]
    only_ids = list(args.only)
    if args.only_file:
        only_ids.extend([line.strip() for line in args.only_file.read_text(encoding="utf-8").splitlines() if line.strip() and not line.startswith("#")])
    only = set(only_ids)
    progress = load_progress(args.progress)
    args.out_dir.mkdir(parents=True, exist_ok=True)

    planned = []
    for ch in characters:
        cid = ch["character_id"]
        if only and cid not in only:
            continue
        for kind in kinds:
            if kind == "banner" and cid in HAS_PROD_BANNER and not args.include_existing_banner:
                continue
            dest = args.out_dir / ("%s_%s.png" % (cid, kind))
            prev = ((progress.get(cid) or {}).get(kind) or {})
            if not args.force and dest.exists() and dest.stat().st_size > 10000:
                planned.append((ch, kind, "skip_local"))
            elif not args.force and prev.get("status") == "ok":
                planned.append((ch, kind, "skip_progress"))
            else:
                planned.append((ch, kind, "run"))

    run_items = [item for item in planned if item[2] == "run"]
    if args.limit:
        allowed = []
        count = 0
        new_planned = []
        for item in planned:
            if item[2] == "run":
                if count < args.limit:
                    new_planned.append(item)
                    allowed.append(item)
                    count += 1
                else:
                    new_planned.append((item[0], item[1], "skip_limit"))
            else:
                new_planned.append(item)
        planned = new_planned
        run_items = allowed

    print("[plan] total=%s run=%s kinds=%s mode=%s quality=%s" % (
        len(planned), len(run_items), kinds, args.mode, args.quality
    ), flush=True)
    if args.dry_run:
        for ch, kind, action in planned:
            print("DRY %-14s %s %s %s" % (action, ch["character_id"], ch["name"], kind), flush=True)
        return 0

    session = requests.Session()
    ok = fail = skip = 0
    for ch, kind, action in planned:
        cid = ch["character_id"]
        name = ch.get("name")
        progress.setdefault(cid, {"name": name})
        if action != "run":
            skip += 1
            print("[skip] %s %s %s (%s)" % (cid, name, kind, action), flush=True)
            continue
        print("[gen] %s %s %s ..." % (cid, name, kind), flush=True)
        info = generate_one(session, args, ch, kind)
        progress[cid][kind] = info
        write_progress(args.progress, progress)
        if info.get("status") == "ok":
            ok += 1
            warn = " " + info["size_warning"] if info.get("size_warning") else ""
            print("[ok] %s %s %s %sx%s %sB %ss%s" % (
                cid, name, kind, info["width"], info["height"], info["bytes"], info["elapsed"], warn
            ), flush=True)
        else:
            fail += 1
            print("[fail] %s %s %s %s" % (cid, name, kind, info.get("error")), flush=True)
        time.sleep(args.sleep)

    print("[done] ok=%s fail=%s skip=%s progress=%s" % (ok, fail, skip, args.progress), flush=True)
    return 1 if fail else 0


if __name__ == "__main__":
    sys.exit(main())
