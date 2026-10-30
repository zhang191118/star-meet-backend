#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Generate the 15 public legacy avatars with one consistent art direction.

The script intentionally reads credentials from the environment. It does not
upload files or change the database; review the generated manifest first, then
run sync_legacy_avatars.py.
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import sys
import time
import urllib.request
from pathlib import Path


IMAGE_URL = os.environ.get(
    "DASHSCOPE_IMAGE_URL",
    "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation",
)
MODEL = os.environ.get("DASHSCOPE_IMAGE_MODEL", "qwen-image-2.0-pro")
ROOT = Path(__file__).resolve().parent
DEFAULT_OUTPUT = ROOT / "avatars_legacy_v2"

STYLE = (
    "高质量半写实动漫角色头像，东方审美，细腻自然的面部结构，柔和电影感布光，"
    "清晰眼神，轻微笔触与真实材质融合，干净简洁的背景，角色正面或三分之二侧脸，"
    "头像主体居中，胸像构图，适合社交应用圆形头像，统一色彩和渲染质感，1024x1024。"
    "不要照片质感，不要真人摄影，不要文字，不要水印，不要 logo，不要多余人物。"
)

CHARACTERS = [
    ("fe9cb634-a684-4b8c-8a1d-9440a73ea4ac", "季夏", "22岁阳光温柔的东亚男性，高中物理成绩优异的班长，黑色微卷短发，清爽眉眼，白衬衫和浅蓝外套，克制的温柔笑意"),
    ("518eae73-b7de-4a82-a0aa-176fc6a84764", "小软", "20岁软糯可爱的东亚女性，蓬松栗色中长发，圆润清澈的眼睛，浅粉色针织衫，害羞又亲近的表情，温柔甜美"),
    ("6f592abc-d07a-4111-a6de-6c4dceeddc81", "江寒", "25岁清冷克制的东亚男性，黑色直发，细长深邃的眼睛，戴简洁黑框眼镜，深灰高领毛衣，理性安静的气质"),
    ("119d02fc-ce7e-410d-9c0f-1b5956dc5160", "红袖", "24岁古典妩媚的东亚女性，乌黑长发用暗红发簪束起，眉眼明艳，朱红色东方风上衣，端庄自信又带一点神秘感"),
    ("4d3b2eb8-2aec-4468-9e5e-e17282b2d822", "阿黄", "一只友善的大型金黄色长毛犬，蓬松柔软的毛发，温暖深棕色眼睛，耳朵自然下垂，忠诚快乐的表情，胸口佩戴素色项圈；必须是狗，不是人类"),
    ("268972af-de0e-420d-b758-5bc5233e1a7e", "御薇", "26岁成熟干练的东亚女性，高马尾，利落眉眼，黑色西装与酒红色内衬，自信从容的御姐气质，精致但不夸张"),
    ("94f0e286-03d4-4413-8761-a0cd1b6b12cc", "知知", "23岁知性安静的东亚女性，深棕色齐肩短发，圆框眼镜，米白色衬衫与浅棕针织背心，温柔聪慧，带有认真倾听的神情"),
    ("2b49ccd4-d926-413d-aff9-5facd20cb861", "娇娇", "20岁傲娇俏皮的东亚女性，长黑发配蝴蝶结发夹，杏眼微微瞪视，粉色学院风毛衣，脸颊自然泛红，精致可爱"),
    ("b854353f-b3ad-44f8-b123-b7ae4dc03366", "甜甜", "19岁元气明亮的东亚女性，高双马尾，明亮大眼睛和小酒窝，黄色休闲上衣，灿烂笑容，清新活泼"),
    ("32dfcc3d-850b-4249-8d96-06039aaf2b8d", "念念", "22岁敏感柔和的东亚女性，黑色长直发轻轻遮住侧脸，眼神依恋而安静，宽松白色毛衣，细腻清秀的气质"),
    ("18a0321b-2160-44af-802f-856d3be6210f", "旭阳", "22岁阳光爽朗的东亚男性，蓬松短发，明亮眼睛和干净笑容，橙色运动外套，少年感和运动活力"),
    ("b6ed088d-c82d-459d-ab4b-93230bd1c662", "沉渊", "28岁成熟稳重的东亚男性，短发带一点自然灰，轮廓深刻，沉静可靠的眼神，深蓝衬衫，温和克制的成熟魅力"),
    ("6131d900-27d0-4c49-b2d0-20588939c2da", "寒川", "25岁禁欲清冷的东亚男性，利落黑发，冷静专注的深色眼睛，黑框眼镜，深灰高领毛衣，极简克制的气质"),
    ("6f0cc81a-76af-46d6-b6bc-22b48f4c1732", "烈风", "27岁强势英俊的东亚男性，短背头，浓眉深目，坚定锐利的眼神，黑色定制西装，成熟精英气质与保护感"),
    ("bea82590-424f-4604-bacf-4391bd1ce3a5", "温言", "26岁温柔沉静的东亚男性，柔软黑发，眉眼清秀，浅色针织衫，轻微含笑的眼神，文雅亲和，给人安全感"),
]


def request_image(prompt: str, api_key: str) -> bytes:
    payload = {
        "model": MODEL,
        "input": {"messages": [{"role": "user", "content": [{"text": prompt}]}]},
        "parameters": {"size": "1024*1024", "n": 1, "prompt_extend": False},
    }
    request = urllib.request.Request(
        IMAGE_URL,
        data=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
        headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=240) as response:
        result = json.loads(response.read().decode("utf-8"))

    content = result["output"]["choices"][0]["message"]["content"][0]
    if isinstance(content, dict) and content.get("image"):
        image_value = content["image"]
    elif isinstance(content, str) and content.startswith("data:image/"):
        image_value = content
    else:
        raise RuntimeError(f"image response did not contain an image: {result.keys()}")

    if image_value.startswith("data:"):
        return base64.b64decode(image_value.split(",", 1)[1])
    with urllib.request.urlopen(image_value, timeout=240) as image_response:
        return image_response.read()


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    parser.add_argument("--only", help="Generate one character id or name")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--retries", type=int, default=4)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    api_key = os.environ.get("DASHSCOPE_API_KEY", "").strip()
    if not args.dry_run and not api_key:
        raise SystemExit("DASHSCOPE_API_KEY is empty; no image was generated")

    args.output.mkdir(parents=True, exist_ok=True)
    manifest = []
    selected = [item for item in CHARACTERS if not args.only or args.only in item[:2]]
    if not selected:
        raise SystemExit(f"unknown character: {args.only}")

    for character_id, name, appearance in selected:
        prompt = f"{STYLE} 角色设定：{appearance}。"
        output = args.output / f"{character_id}.png"
        entry = {"character_id": character_id, "name": name, "prompt": prompt, "file": str(output)}
        manifest.append(entry)
        if args.dry_run:
            print(f"[dry-run] {name}: {output}")
            continue
        if output.exists() and output.stat().st_size > 20_000:
            print(f"[skip] {name}: {output}")
            continue
        for attempt in range(1, args.retries + 1):
            try:
                output.write_bytes(request_image(prompt, api_key))
                print(f"[ok] {name}: {output} ({output.stat().st_size} bytes)")
                break
            except Exception as error:
                if attempt == args.retries:
                    raise RuntimeError(f"{name} failed after {attempt} attempts: {error}") from error
                print(f"[retry {attempt}/{args.retries}] {name}: {error}", file=sys.stderr)
                time.sleep(min(30, attempt * 5))

    manifest_path = args.output / "manifest.json"
    manifest_path.write_text(json.dumps({"model": MODEL, "entries": manifest}, ensure_ascii=False, indent=2) + "\n")
    print(f"manifest: {manifest_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
