#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""为8个角色生成超写实方形头像图 (qwen-image-2.0-pro)"""
import json, os, sys, time, urllib.request

API_KEY = os.environ.get("DASHSCOPE_API_KEY", "").strip()
if not API_KEY:
    raise SystemExit("DASHSCOPE_API_KEY is required")
URL = "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"
OUT_DIR = os.path.join(os.path.dirname(__file__), "avatars_new")
os.makedirs(OUT_DIR, exist_ok=True)

STYLE = "超写实摄影级人像头像,真实皮肤质感,毛孔和细微纹理清晰可见,85mm定焦镜头浅景深虚化,自然柔和的布光,电影级调色,高分辨率细节,正方形头像构图,面部特写,人物居中,不要任何文字水印,不要动漫风格,不要插画风格,不要二次元"

CHARS = [
    ("6f0cc81a-76af-46d6-b6bc-22b48f4c1732", "烈",
     "一位极具魅力的年轻亚洲男性,27岁,短背头发型利落精致,浓眉深目,眼神强势而深邃带着占有欲,轮廓分明的英俊脸庞,高挺鼻梁,薄唇微抿,穿着黑色定制西装,气场强大,真实男性面容,商务精英气质",
     "正面头像特写,柔和自然光"),
    ("6131d900-27d0-4c49-b2d0-20588939c2da", "寒",
     "一位清冷的年轻亚洲男性,25岁,黑色直发干净利落略带自然碎感,面无表情但眼神专注深邃,剑眉星目,戴着黑框眼镜,穿着深灰色高领毛衣,皮肤白皙,真实男性面容,禁欲冷峻气质",
     "正面头像特写,清冷光线下"),
    ("b6ed088d-c82d-459d-ab4b-93230bd1c662", "沉",
     "一位成熟稳重的亚洲男性,28岁,整洁的短发带一点灰白,浓眉,眼神沉稳可靠而温和,脸部线条成熟硬朗,穿着深蓝色衬衫,真实男性面容,成熟魅力",
     "正面头像特写,暖色侧光"),
    ("18a0321b-2160-44af-802f-856d3be6210f", "小阳",
     "一位阳光活泼的亚洲少年,22岁,蓬松自然的短发,浓眉大眼闪闪发亮,笑容灿烂充满少年感,脸部线条干净清爽,穿着橙色连帽卫衣,真实男性面容,帅气阳光",
     "正面头像特写,明亮自然光"),
    ("32dfcc3d-850b-4249-8d96-06039aaf2b8d", "念念",
     "一位柔弱的年轻亚洲女性,22岁,黑色长直发微微遮住半边脸,大眼睛眼神带着不安和依恋,皮肤白皙细腻,樱桃小嘴,穿着oversize白色毛衣,楚楚动人的真实女性面容,清秀柔美",
     "正面头像特写,柔和阴天光线"),
    ("b854353f-b3ad-44f8-b123-b7ae4dc03366", "甜甜",
     "一位阳光元气的亚洲少女,19岁,双马尾扎得高高的,圆圆的大眼睛,笑容灿烂如阳光,露出小酒窝,皮肤健康光泽,穿着黄色T恤,甜美可爱的真实女性面容,元气满满",
     "正面头像特写,阳光明媚光线"),
    ("2b49ccd4-d926-413d-aff9-5facd20cb861", "娇娇",
     "一位精致的亚洲年轻女性,20岁,长直发别着蝴蝶结发夹,微微嘟嘴,脸颊泛着自然的微红,杏眼微嗔,穿着粉色学院风毛衣,精致可爱的真实女性面容,傲娇俏皮",
     "正面头像特写,柔和粉色光晕"),
    ("268972af-de0e-420d-b758-5bc5233e1a7e", "御",
     "一位成熟干练的亚洲职场女性,26岁,高马尾发型利落,锐利自信的丹凤眼,精致美艳的五官,高颧骨,薄唇微抿带着气场,穿着深色西装外套,气场强大,真实女性面容,御姐气质",
     "正面头像特写,冷色调光线"),
]

def gen(prompt):
    body = json.dumps({
        "model": "qwen-image-2.0-pro",
        "input": {"messages": [{"role": "user", "content": [{"text": prompt}]}]},
        "parameters": {"size": "1024*1024", "n": 1, "prompt_extend": False},
    }).encode()
    req = urllib.request.Request(URL, data=body, headers={
        "Authorization": "Bearer " + API_KEY,
        "Content-Type": "application/json",
    })
    with urllib.request.urlopen(req, timeout=180) as resp:
        data = json.loads(resp.read())
    return data["output"]["choices"][0]["message"]["content"][0]["image"]

def main():
    only = sys.argv[1] if len(sys.argv) > 1 else None
    for cid, name, appearance, scene in CHARS:
        if only and only != name:
            continue
        out_path = os.path.join(OUT_DIR, cid + ".png")
        if os.path.exists(out_path) and os.path.getsize(out_path) > 10000:
            print(f"[skip] {name} exists", flush=True)
            continue
        prompt = appearance + "。场景:" + scene + "。" + STYLE
        print(f"[gen] {name} ...", flush=True)
        success = False
        for attempt in range(5):
            try:
                img_url = gen(prompt)
                urllib.request.urlretrieve(img_url, out_path)
                size = os.path.getsize(out_path)
                print(f"[ok] {name} -> {out_path} ({size} bytes)", flush=True)
                success = True
                break
            except Exception as e:
                print(f"[retry {attempt+1}] {name}: {e}", flush=True)
                time.sleep(10)
        if not success:
            print(f"[fail] {name}", flush=True)
        time.sleep(3)
    print("DONE", flush=True)

if __name__ == "__main__":
    main()
