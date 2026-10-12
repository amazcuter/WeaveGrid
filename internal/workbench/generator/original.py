#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0
"""经纬传播 · 骨架生成器 v2（合成测试数据）

v2 相对 v1 的改动：
  「纬线两头必须都是经线」这条约束**太紧**——真实数据实测，741 条线里若强制两头都挂经线
  （经线=度≥5 的章），只剩 171 条（23%），图会稀得完全不像。
  ⇒ 改为：**纬线两端可以是任意章，只要求「间隔 ≥ 2 章」**。
     经线退化成故事里的**转折点**（收账发生的地方），不再是线的准入条件。

规则：
  1. 纬线两端 = 任意章；硬约束只有「间隔 ≥ 2 章」
  2. 倒着填：先定转折点（「哪里要炸」），再往回挑「该在哪埋」
  3. 跨度 > 25 章的纬线要打结（remind）
  4. 粒度：只给「发生了什么不可逆的事」

输出：
  jingwei_<N>ch.json       完整结构（经线 + 纬线 + 节拍）
  jingwei_<N>ch_lines.json   兼容格式 [[埋, 收], ...]（去重）

用法：python3 original.py --chapters 50 --seed 7 --out exports/structure
"""
import argparse
import json
import os
import random
from collections import Counter

MIN_GAP = 2            # 对齐真实数据：真实数据最短间隔就是 2（没有 gap 0/1）

THREAD_TYPES = ["身份", "物品", "能力设定", "人物关系", "剧情走向"]
TYPE_W = [50, 11, 17, 14, 8]

CLUE_POOL = [
    "一只旧钥匙", "失踪的哥哥", "那枚徽章", "师父的遗言", "编号 002 的规则",
    "雨夜的车祸", "会说话的乌鸦", "半张地图", "左手腕的疤", "一封没寄出的信",
    "青训营的落榜", "旧仓库的地下室", "禁忌之地的路标", "女孩的生日", "那把断刀",
    "茶楼的老账本", "第七封信", "教堂的钟声", "同桌的笔记", "那年的雪",
    "不该存在的照片", "药剂的副作用", "影子候选者", "禁令的例外", "沉默的证人",
]

ARC_OPEN_EVENT = [
    "主角接下新任务，进入陌生地界（新身份/新场景展开）",
    "一个新组织登场，抛出合作条件",
    "主角获得一项此前没有的能力，但代价未明",
    "与新的对手正面照面，双方都留了底牌",
    "一个旧人物以新身份出现",
]
ARC_CLOSE_EVENT = [
    "约定破裂，一方撕破脸",
    "某人身份当场曝光",
    "关键人物死亡，局势不可逆",
    "真相揭底，主角的选择被逼到墙角",
    "一次正面对决分出胜负，格局重排",
]
MIXED_EVENT = [
    "一边结掉前面的账，一边抛出新的钩子",
    "对手倒下，但更大的势力浮出水面",
    "主角做出不可逆的选择，同时发现新的谜团",
]


def weighted(rng, items, weights):
    return rng.choices(items, weights=weights, k=1)[0]


def build_arcs(rng, total):
    arcs, pos = [], 1
    while pos <= total:
        span = rng.randint(8, 14)
        end = min(pos + span - 1, total)
        if total - end < 6:
            end = total
        arcs.append((pos, end))
        pos = end + 1
    return arcs


def gen(chapters, seed):
    rng = random.Random(seed)
    arcs = build_arcs(rng, chapters)

    # ---------------- ① 布经线 ----------------
    nodes = []
    for i, (s, e) in enumerate(arcs, 1):
        nodes.append(dict(ch=min(s + rng.randint(0, 2), e), arc=i, kind="开线",
                          event=rng.choice(ARC_OPEN_EVENT)))
        if e - s >= 10 and rng.random() < 0.5:
            nodes.append(dict(ch=rng.randint(s + 4, e - 3), arc=i, kind="混合",
                              event=rng.choice(MIXED_EVENT)))
        nodes.append(dict(ch=max(e - rng.randint(0, 2), s), arc=i, kind="结账",
                          event=rng.choice(ARC_CLOSE_EVENT)))
    pri = {"结账": 0, "混合": 1, "开线": 2}
    bych = {}
    for n in nodes:
        if n["ch"] not in bych or pri[n["kind"]] < pri[bych[n["ch"]]["kind"]]:
            bych[n["ch"]] = n
    nodes = sorted(bych.values(), key=lambda n: n["ch"])
    node_chs = [n["ch"] for n in nodes]

    # ---------------- ② 倒着填纬线 ----------------
    threads, used = [], set()
    tid = 0

    def make_thread(plant, payoff, tag):
        nonlocal tid
        if payoff - plant < MIN_GAP:
            return None
        tid += 1
        clue = rng.choice(CLUE_POOL)
        tries = 0
        while clue in used and len(used) < len(CLUE_POOL) and tries < 40:
            clue = rng.choice(CLUE_POOL)
            tries += 1
        used.add(clue)
        span = payoff - plant
        reminds = []
        if span > 25:
            step = rng.randint(14, 22)
            c = plant + rng.randint(5, step)
            while c < payoff - 3:
                if c not in node_chs:
                    reminds.append(c)
                c += step
        return dict(id=f"T{tid:03d}", clue=clue,
                    type=weighted(rng, THREAD_TYPES, TYPE_W),
                    plant=plant, reminds=sorted(reminds), payoff=payoff,
                    span=span, tag=tag)

    # ② -a 骨架线：每个转折点（回收端）往回挑埋点 —— 埋点可以是任意更早的章
    for ni, n in enumerate(nodes):
        if ni == 0:
            continue
        if n["kind"] == "开线":
            k = rng.randint(0, 1)
        elif n["kind"] == "混合":
            k = rng.randint(2, 4)
        else:
            k = rng.randint(3, 6)
        for _ in range(k):
            r = rng.random()
            if r < 0.65 and node_chs[:ni]:          # 埋在更早的经线上（短跨度为主）
                plant = rng.choice(node_chs[:ni])
            else:                                    # 任意更早的普通章
                back = min(n["ch"] - MIN_GAP, rng.choice([3, 5, 8, 14, 25, 45]))
                plant = max(1, n["ch"] - back)
            t = make_thread(plant, n["ch"], "骨架")
            if t:
                threads.append(t)

    # ② -b 普通呼应线：两端都不是经线。真实 50 章切片里「间隔 2」占 24%、是最多的一档
    n_local = int(chapters * 0.55)
    for _ in range(n_local):
        a = rng.randint(1, chapters - MIN_GAP)
        if rng.random() < 0.25:
            d = MIN_GAP                                # 相邻感最强的那一档
        else:
            d = MIN_GAP + int(rng.expovariate(1 / 4))
        b = min(chapters, a + min(d, 22))
        if b - a < MIN_GAP:
            continue
        t = make_thread(a, b, "普通")
        if t:
            threads.append(t)

    threads.sort(key=lambda t: (t["payoff"], t["plant"]))
    for i, t in enumerate(threads, 1):
        t["id"] = f"T{i:03d}"

    # ---------------- ③ 剖面 ----------------
    af, at = Counter(), Counter()
    for t in threads:
        af[t["plant"]] += 1
        at[t["payoff"]] += 1
    for n in nodes:
        n["埋"] = af[n["ch"]]
        n["收"] = at[n["ch"]]
        n["度"] = af[n["ch"]] + at[n["ch"]]

    deg_all = Counter()
    for t in threads:
        deg_all[t["plant"]] += 1
        deg_all[t["payoff"]] += 1

    return dict(chapters=chapters, seed=seed, min_gap=MIN_GAP,
                arcs=[list(a) for a in arcs], nodes=nodes, threads=threads,
                degree={str(k): v for k, v in sorted(deg_all.items())})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--chapters", type=int, default=50)
    ap.add_argument("--seed", type=int, default=7)
    ap.add_argument("--out", default="exports/structure")
    a = ap.parse_args()
    os.makedirs(a.out, exist_ok=True)

    d = gen(a.chapters, a.seed)
    n = a.chapters
    p1 = os.path.join(a.out, f"jingwei_{n}ch.json")
    p2 = os.path.join(a.out, f"jingwei_{n}ch_lines.json")
    json.dump(d, open(p1, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    lines = sorted({(t["plant"], t["payoff"]) for t in d["threads"]})
    json.dump([list(x) for x in lines], open(p2, "w", encoding="utf-8"), ensure_ascii=False)

    sp = sorted(t["span"] for t in d["threads"])
    onode = {nd["ch"] for nd in d["nodes"]}
    both = sum(1 for t in d["threads"] if t["plant"] in onode and t["payoff"] in onode)
    deg = [v for v in d["degree"].values()]
    print(f"章数 {n} · 篇章 {len(d['arcs'])} 段 · 经线 {len(d['nodes'])} 个 · 纬线 {len(d['threads'])} 条")
    print(f"硬约束：间隔 ≥ {d['min_gap']} 章（对齐真实数据下限）")
    print(f"跨度：min {sp[0]}  中位 {sp[len(sp)//2]}  max {sp[-1]}")
    print(f"其中骨架线 {sum(1 for t in d['threads'] if t['tag']=='骨架')} 条 / 普通呼应线 {sum(1 for t in d['threads'] if t['tag']=='普通')} 条")
    print(f"两端都正好是经线的：{both} 条（{both/len(d['threads'])*100:.0f}%）  ← 这个比例才像真实数据")
    print(f"涉及章数 {len(deg)}/{n}   中位度 {sorted(deg)[len(deg)//2]}  最大度 {max(deg)}")
    print(f"唯一对 {len(lines)} 个")
    print(f"\n-> {p1}\n-> {p2}")


if __name__ == "__main__":
    main()
