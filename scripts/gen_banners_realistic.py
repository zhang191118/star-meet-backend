#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""为8个角色重新生成超写实摄影风格横幅图 (qwen-image-2.0-pro)"""
import json, os, sys, time, urllib.request

API_KEY = os.environ.get("DASHSCOPE_API_KEY", "").strip()
if not API_KEY:
    raise SystemExit("DASHSCOPE_API_KEY is required")
URL = "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"
OUT_DIR = os.path.join(os.path.dirname(__file__), "banners")
os.makedirs(OUT_DIR, exist_ok=True)

# 超写实摄影风格 - 强调真实人像质感
STYLE = "超写实摄影级人像,真实皮肤质感,毛孔和细微纹理清晰可见,85mm定焦镜头浅景深虚化,自然柔和的布光,电影级调色,高分辨率细节,横版宽幅构图,人物位于画面右侧三分之二处,画面左侧保持柔和的背景虚化留白,不要任何文字水印,不要动漫风格,不要插画风格,不要二次元"

CHARS = [
    ("6f0cc81a-76af-46d6-b6bc-22b48f4c1732", "烈",
     "一位极具魅力的年轻亚洲男性,27岁,短背头发型利落精致,浓眉深目,眼神强势而深邃带着占有欲,轮廓分明的英俊脸庞,高挺鼻梁,薄唇微抿,穿着剪裁合体的黑色定制西装,内搭深色衬衫解开两粒扣,气场强大,真实男性面容,商务精英气质",
     "顶层办公室,巨幅落地窗外是黄昏城市天际线,暖色调光线从窗外洒入,他解开袖扣靠在办公桌旁,眼神强势"),
    ("6131d900-27d0-4c49-b2d0-20588939c2da", "寒",
     "一位清冷的年轻亚洲男性,25岁,黑色直发干净利落略带自然碎感,面无表情但眼神专注深邃,剑眉星目,戴着黑框眼镜,穿着深灰色高领毛衣,侧脸线条清冷俊朗,皮肤白皙,真实男性面容,禁欲冷峻气质",
     "深夜的办公室,多块显示器泛着冷蓝色光,他坐在椅子上专注看着屏幕,侧脸被屏幕光映照,清冷氛围"),
    ("b6ed088d-c82d-459d-ab4b-93230bd1c662", "沉",
     "一位成熟稳重的亚洲男性,28岁,整洁的短发带一点灰白,浓眉,眼神沉稳可靠而温和,脸部线条成熟硬朗,穿着深蓝色衬衫袖口卷起,气质如大树般稳重可靠,真实男性面容,成熟魅力",
     "深夜的书房,暖光台灯下,他端着咖啡坐在皮质沙发上温和微笑,身后是整齐的实木书架,温暖氛围"),
    ("18a0321b-2160-44af-802f-856d3be6210f", "小阳",
     "一位阳光活泼的亚洲少年,22岁,蓬松自然的短发,浓眉大眼闪闪发亮,笑容灿烂充满少年感,脸部线条干净清爽,穿着橙色连帽卫衣,青春洋溢的真实男性面容,帅气阳光",
     "傍晚的篮球场,晚霞满天橙红色光芒,他抱着篮球站在场边咧嘴大笑,青春洋溢"),
    ("32dfcc3d-850b-4249-8d96-06039aaf2b8d", "念念",
     "一位柔弱的年轻亚洲女性,22岁,黑色长直发微微遮住半边脸,大眼睛眼神带着不安和依恋,皮肤白皙细腻,樱桃小嘴,穿着oversize白色毛衣,楚楚动人的真实女性面容,清秀柔美",
     "安静卧室的窗边,阴天柔和的自然光从纱帘透入,她抱着毛绒兔子玩偶坐在窗台上,眼神依恋"),
    ("b854353f-b3ad-44f8-b123-b7ae4dc03366", "甜甜",
     "一位阳光元气的亚洲少女,19岁,双马尾扎得高高的,圆圆的大眼睛,笑容灿烂如阳光,露出小酒窝,皮肤健康光泽,穿着明亮的黄色T恤和牛仔背带裙,甜美可爱的真实女性面容,元气满满",
     "夏日游乐园,彩色气球和旋转木马背景虚化,她举着冰淇淋灿烂大笑,阳光明媚"),
    ("2b49ccd4-d926-413d-aff9-5facd20cb861", "娇娇",
     "一位精致的亚洲年轻女性,20岁,长直发别着蝴蝶结发夹,微微嘟嘴,脸颊泛着自然的微红,杏眼微嗔,穿着粉色学院风毛衣,精致可爱的真实女性面容,傲娇俏皮",
     "樱花飘落的校园小径,柔和的粉色光晕中,她抱着书本微微嘟嘴,脸颊微红故作傲娇"),
    ("268972af-de0e-420d-b758-5bc5233e1a7e", "御",
     "一位成熟干练的亚洲职场女性,26岁,高马尾发型利落,锐利自信的丹凤眼,精致美艳的五官,高颧骨,薄唇微抿带着气场,穿着深色西装外套内搭真丝衬衫,气场强大,真实女性面容,御姐气质",
     "高层办公室落地窗前,城市夜景灯火璀璨虚化在背景中,她抱着手臂自信而立,冷艳气场"),
]

def gen(prompt):
    body = json.dumps({
        "model": "qwen-image-2.0-pro",
        "input": {"messages": [{"role": "user", "content": [{"text": prompt}]}]},
        "parameters": {"size": "1664*928", "n": 1, "prompt_extend": False},
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
        # Force regenerate - delete existing
        if os.path.exists(out_path):
            os.remove(out_path)
        prompt = appearance + "。场景:" + scene + "。" + STYLE
        print(f"[gen] {name} ...", flush=True)
        success = False
        for attempt in range(3):
            try:
                img_url = gen(prompt)
                urllib.request.urlretrieve(img_url, out_path)
                size = os.path.getsize(out_path)
                print(f"[ok] {name} -> {out_path} ({size} bytes)", flush=True)
                success = True
                break
            except Exception as e:
                print(f"[retry {attempt+1}] {name}: {e}", flush=True)
                time.sleep(5)
        if not success:
            print(f"[fail] {name}", flush=True)
        time.sleep(1)
    print("DONE", flush=True)

if __name__ == "__main__":
    main()
