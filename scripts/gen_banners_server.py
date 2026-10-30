#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import json, os, subprocess, time, uuid, urllib.request

API_KEY = os.environ.get("DASHSCOPE_API_KEY", "").strip()
GEN_URL = "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"
API = "http://127.0.0.1:8080/api/v1"
TOKEN = os.environ.get("STARMEET_ADMIN_TOKEN", "").strip()
DB_PWD = os.environ.get("MYSQL_ROOT_PASSWORD", "").strip()
if not API_KEY or not TOKEN or not DB_PWD:
    raise SystemExit("DASHSCOPE_API_KEY, STARMEET_ADMIN_TOKEN, and MYSQL_ROOT_PASSWORD are required")
DIR = "/root/banners"
os.makedirs(DIR, exist_ok=True)

STYLE = "半写实动漫插画风格,细腻柔和的上色,电影感光影,高质量细节,横版宽幅构图,人物位于画面右侧三分之二处,画面左侧保持干净柔和的背景留白,不要任何文字水印"

CHARS = [
    ("fe9cb634-a684-4b8c-8a1d-9440a73ea4ac", "季夏", "阳光少年,微卷短发,笑容温暖明亮,穿着白色T恤和浅蓝衬衫外套,背着吉他包", "夏日校园的林荫草坪上,午后阳光透过树叶洒下光斑,他坐在草地上抱着木吉他弹唱"),
    ("518eae73-b7de-4a82-a0aa-176fc6a84764", "小软", "温柔少女,及肩微卷发,眼神柔和带着依赖,穿着淡紫色针织衫,怀里抱着素描本", "安静的美术教室窗边,夕阳暖光洒入,她抱着素描本微微侧头微笑"),
    ("6f592abc-d07a-4111-a6de-6c4dceeddc81", "江寒", "忧郁青年,黑色碎发微微遮眉,眼神深邃带着忧郁,穿着深色高领毛衣,气质冷冽", "雨夜的城市天台,霓虹灯光晕染在湿润的空气中,他倚着栏杆望向远方"),
    ("119d02fc-ce7e-410d-9c0f-1b5956dc5160", "红袖", "红色兜帽衫少女,短发微卷,眼神灵动带着一丝狡黠,身边总跟着一只橘猫", "黄昏的老街巷口,暖黄路灯下,她蹲着逗一只橘猫,笑容俏皮"),
    ("4d3b2eb8-2aec-4468-9e5e-e17282b2d822", "阿黄", "一只大型白色长毛犬,大白熊犬混血,圆圆的眼睛温柔忠诚,毛发蓬松", "春日公园的草坪上,阳光灿烂,大白熊犬吐着舌头欢快奔跑,毛发蓬松发亮"),
    ("268972af-de0e-420d-b758-5bc5233e1a7e", "御", "职场御姐,高马尾,眼神锐利带着自信,穿着深色西装外套,气场强大", "高层办公室落地窗前,城市夜景灯火璀璨,她抱着手臂自信而立"),
    ("94f0e286-03d4-4413-8761-a0cd1b6b12cc", "知知", "知性女性,齐肩短发干净利落,戴着细框眼镜,温柔从容的微笑,穿着米色高领毛衣", "温暖的书房里,木质书架和暖光台灯,她捧着一杯热茶安静微笑"),
    ("2b49ccd4-d926-413d-aff9-5facd20cb861", "娇娇", "傲娇少女,长直发别着蝴蝶结发夹,微微嘟嘴,脸颊泛红,穿着粉色学院风毛衣", "樱花飘落的校园小径,她抱着书本微微嘟嘴,脸颊泛红故作傲娇"),
    ("b854353f-b3ad-44f8-b123-b7ae4dc03366", "甜甜", "阳光少女,双马尾,笑容灿烂如阳光,穿着明亮的黄色T恤和牛仔背带裙", "夏日游乐园,彩色气球和旋转木马背景,她举着冰淇淋灿烂大笑"),
    ("32dfcc3d-850b-4249-8d96-06039aaf2b8d", "念念", "柔弱少女,黑色长发微微遮眼,眼神带着不安和依恋,穿着oversize白色毛衣", "安静卧室的窗边,阴天柔和的光线,她抱着毛绒兔子玩偶,眼神依恋"),
    ("18a0321b-2160-44af-802f-856d3be6210f", "小阳", "活泼少年,蓬松短发,大眼睛闪闪发亮,笑容灿烂带着少年感,穿着橙色卫衣", "傍晚的篮球场,晚霞满天,他抱着篮球咧嘴大笑,青春洋溢"),
    ("b6ed088d-c82d-459d-ab4b-93230bd1c662", "沉", "成熟男性,整洁短发带一点灰白,眼神沉稳可靠,穿着深蓝色衬衫,气质如大树般稳重", "深夜的书房,暖光台灯下,他端着咖啡温和微笑,身后是整齐的书架"),
    ("6131d900-27d0-4c49-b2d0-20588939c2da", "寒", "清冷青年,黑色直发干净利落,面无表情但眼神专注,穿着深灰色高领毛衣和黑框眼镜", "深夜的办公室里,多块显示器泛着冷光,他专注敲代码,侧脸清冷"),
    ("6f0cc81a-76af-46d6-b6bc-22b48f4c1732", "烈", "霸道总裁,短背头,眼神强势带着占有欲,穿着黑色定制西装,气场全开", "顶层办公室,巨幅落地窗外是城市天际线,他解开袖扣眼神强势"),
    ("bea82590-424f-4604-bacf-4391bd1ce3a5", "温", "温柔青年,浅棕色柔顺短发,眼神温暖包容,穿着米色开衫毛衣,气质如沐春风", "洒满阳光的阳台,绿植环绕,他浇着花温柔微笑,春风拂面"),
]

def gen(prompt):
    body = json.dumps({
        "model": "qwen-image-2.0-pro",
        "input": {"messages": [{"role": "user", "content": [{"text": prompt}]}]},
        "parameters": {"size": "1664*928", "n": 1, "prompt_extend": False},
    }).encode()
    req = urllib.request.Request(GEN_URL, data=body, headers={
        "Authorization": "Bearer " + API_KEY,
        "Content-Type": "application/json",
    })
    with urllib.request.urlopen(req, timeout=180) as resp:
        data = json.loads(resp.read())
    return data["output"]["choices"][0]["message"]["content"][0]["image"]

def upload(path):
    boundary = uuid.uuid4().hex
    with open(path, "rb") as f:
        payload = f.read()
    body = (("--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"banner.png\"\r\nContent-Type: image/png\r\n\r\n").encode()
            + payload + ("\r\n--" + boundary + "--\r\n").encode())
    req = urllib.request.Request(API + "/character/upload-image", data=body, headers={
        "Authorization": "Bearer " + TOKEN,
        "Content-Type": "multipart/form-data; boundary=" + boundary,
    })
    with urllib.request.urlopen(req, timeout=120) as resp:
        d = json.loads(resp.read())
    if d.get("code") != 200:
        raise RuntimeError(str(d))
    return d["data"]["url"]

def set_db(cid, url):
    sql = "UPDATE characters SET banner_image='" + url + "' WHERE character_id='" + cid + "';"
    subprocess.run(["docker", "exec", "-i", "starmeet-mysql", "mysql", "-uroot", "-p" + DB_PWD, "star_meet", "-e", sql], check=True, capture_output=True)

def main():
    for cid, name, appearance, scene in CHARS:
        p = os.path.join(DIR, cid + ".png")
        ok = os.path.exists(p) and os.path.getsize(p) > 10000
        if not ok:
            for attempt in range(3):
                try:
                    img = gen(appearance + "。场景:" + scene + "。" + STYLE)
                    urllib.request.urlretrieve(img, p)
                    ok = True
                    print("[gen] " + name + " ok", flush=True)
                    break
                except Exception as e:
                    print("[retry] " + name + ": " + str(e), flush=True)
                    time.sleep(5)
        if not ok:
            print("[fail] " + name, flush=True)
            continue
        url = upload(p)
        set_db(cid, url)
        print("[done] " + name + " -> " + url, flush=True)
        time.sleep(1)
    print("ALL DONE", flush=True)

if __name__ == "__main__":
    main()
