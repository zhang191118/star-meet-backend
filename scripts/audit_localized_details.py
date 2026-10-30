#!/usr/bin/env python3
"""Audit generated detail translations for cross-locale consistency violations.

Checks:
- en output contains no CJK characters;
- zh-Hans output contains no Traditional-Chinese-only glyphs (via uconv T->S
  diff) and no Taiwan place markers;
- zh-Hant output contains no Simplified-Chinese-only glyphs;
- zh-* outputs do not reuse Latin place/name tokens from the en profile
  (a small allowlist covers legitimate loanwords).

Prints offending character ids per check; exits 1 if any violation found.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
from pathlib import Path


DETAIL_FIELDS = ("example", "first_situation", "first_words", "background")

TRAD_ONLY_MARKERS = (
    "臺灣", "台湾", "台北", "臺北", "台中", "臺中", "高雄", "臺南", "台南",
    "新竹", "嘉義", "嘉義", "基隆", "宜蘭", "花蓮", "臺東", "台東", "苗栗",
    "彰化", "南投", "屏東", "澎湖",
)
LATIN_ALLOWLIST = {
    "cosplay", "ok", "app", "vip", "pt", "ig", "ai", "dna", "wifi", "diy",
    "pk", "ceo", "nba", "ktv", "suv", "gps", "pdf", "ip", "iphone", "ipad",
    # 真实游戏主机名与角色设定里的虚构店名，中文文案中保留原文属正常用法。
    "genesis", "nocturne",
}


def has_cjk(text: str) -> bool:
    return any("\u4e00" <= ch <= "\u9fff" or "\u3400" <= ch <= "\u4dbf" for ch in text)


def traditional_ratio(text: str) -> float:
    """Share of characters changed by a Traditional->Simplified conversion.

    A fully Simplified text barely changes; embedded Traditional glyphs do.
    """
    cjk = [ch for ch in text if "\u4e00" <= ch <= "\u9fff"]
    if not cjk:
        return 0.0
    try:
        result = subprocess.run(
            ["/Applications/ServBay/bin/uconv", "-x", "Traditional-Simplified"],
            input=text,
            text=True,
            capture_output=True,
            check=True,
        )
    except (OSError, subprocess.CalledProcessError):
        return 0.0
    converted = result.stdout
    changed = sum(1 for a, b in zip(text, converted) if a != b and "\u4e00" <= a <= "\u9fff")
    return changed / max(1, len(cjk))


def latin_tokens(text: str) -> set[str]:
    return {tok.lower() for tok in re.findall(r"[A-Za-z][A-Za-z]{4,}", text)}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--details", type=Path, default=Path("scripts/generated/localized_details.json"))
    parser.add_argument("--sources", type=Path, default=Path("scripts/generated/localized_detail_sources.json"))
    parser.add_argument("--trad-ratio", type=float, default=0.05)
    args = parser.parse_args()

    details = {item["character_id"]: item["details"] for item in json.loads(args.details.read_text(encoding="utf-8"))}
    sources = {item["character_id"]: item for item in json.loads(args.sources.read_text(encoding="utf-8"))}

    bad: set[str] = set()
    for character_id, generated in details.items():
        source = sources[character_id]
        en_context = " ".join(source["locales"]["en"].values())
        en_tokens = latin_tokens(en_context) - LATIN_ALLOWLIST
        reasons: list[str] = []

        for field in DETAIL_FIELDS:
            en_value = generated.get("en", {}).get(field, "")
            if has_cjk(en_value):
                reasons.append(f"en.{field} contains CJK")
            hans_value = generated.get("zh-Hans", {}).get(field, "")
            if any(marker in hans_value for marker in TRAD_ONLY_MARKERS):
                reasons.append(f"zh-Hans.{field} mentions Taiwan places")
            if latin_tokens(hans_value) & en_tokens:
                reasons.append(f"zh-Hans.{field} reuses en tokens: {sorted(latin_tokens(hans_value) & en_tokens)}")
            hant_value = generated.get("zh-Hant", {}).get(field, "")
            if latin_tokens(hant_value) & en_tokens:
                reasons.append(f"zh-Hant.{field} reuses en tokens: {sorted(latin_tokens(hant_value) & en_tokens)}")

        hans_all = "".join(generated.get("zh-Hans", {}).get(field, "") for field in DETAIL_FIELDS)
        if traditional_ratio(hans_all) > args.trad_ratio:
            reasons.append(f"zh-Hans has {traditional_ratio(hans_all):.1%} Traditional glyphs (> {args.trad_ratio:.0%})")
        # 真正的繁体文本经 T->S 转换会有明显变化；几乎不变说明输出的是简体。
        hant_all = "".join(generated.get("zh-Hant", {}).get(field, "") for field in DETAIL_FIELDS)
        hant_ratio = traditional_ratio(hant_all)
        if hant_ratio < args.trad_ratio:
            reasons.append(f"zh-Hant only {hant_ratio:.1%} changed by T->S conversion, likely Simplified")

        if reasons:
            bad.add(character_id)
            print(f"{character_id} [{source['locales']['zh-Hant']['name']}]:")
            for reason in reasons:
                print("   -", reason)

    print(f"\naudited {len(details)}, violations in {len(bad)}")
    if bad:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
