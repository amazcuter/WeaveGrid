"""结构输出入口：保留原始随机序列，补足末章收束后按回收入度分段。"""
import argparse
import json
from bisect import bisect_left
from collections import Counter
from pathlib import Path


def close_final_chapter(raw):
    """每条独立伏笔计一个入度；末章至少回收五条，优先补不同来源。"""
    final = raw["chapters"]
    sources = Counter(t["plant"] for t in raw["threads"] if t["payoff"] == final)
    for _ in range(max(0, 5 - sum(sources.values()))):
        plant = min(range(1, final - 1), key=lambda ch: (sources[ch], final - ch))
        raw["threads"].append(dict(plant=plant, payoff=final, reminds=[],
                                   span=final - plant, tag="骨架"))
        sources[plant] += 1
    raw["threads"].sort(key=lambda t: (t["payoff"], t["plant"]))
    for i, thread in enumerate(raw["threads"], 1):
        thread["id"] = f"T{i:03d}"


def divide_by_indegree(raw):
    """入度大于四的章节归入前一单元，下一章才开始新单元。"""
    incoming = Counter(t["payoff"] for t in raw["threads"])
    ends = sorted(ch for ch, degree in incoming.items() if degree > 4)
    raw["arcs"] = [[start + 1, end] for start, end in zip([0] + ends, ends)]
    nodes = {node["ch"]: node for node in raw["nodes"]}
    for end in ends:
        nodes.setdefault(end, dict(ch=end, kind="结账"))
    raw["nodes"] = sorted(nodes.values(), key=lambda node: node["ch"])
    for node in raw["nodes"]:
        node["arc"] = bisect_left(ends, node["ch"]) + 1


def generate(chapters, seed):
    if not 3 <= chapters <= 2000:
        raise ValueError("自动生成的章节数必须在 3–2000 之间")
    namespace = {"__name__": "jingwei_library"}
    source = Path(__file__).with_name("original.py")
    exec(compile(source.read_text(), str(source), "exec"), namespace)
    raw = namespace["gen"](chapters, seed)
    close_final_chapter(raw)
    divide_by_indegree(raw)
    turning = {n["ch"] for n in raw["nodes"]}
    fixed = []
    for t in raw["threads"]:
        if t["span"] > 25 and not t["reminds"]:
            choices = [c for c in range(t["plant"] + 1, t["payoff"]) if c not in turning]
            c = min(choices, key=lambda c: (abs(c * 2 - t["plant"] - t["payoff"]), c)) if choices else (t["plant"] + t["payoff"]) // 2
            t["reminds"] = [c]
            fixed.append(t["id"])
    # 只允许结构字段出现在输出中；随机线索、事件和类别从未进入项目材料。
    return dict(chapters=chapters, seed=seed, minGap=2, generatorVersion="jingwei-v2-indegree.1",
                arcs=raw["arcs"], nodes=[{k: n[k] for k in ("ch", "arc", "kind")} for n in raw["nodes"]],
                threads=[{k: t[k] for k in ("id", "plant", "payoff", "reminds", "span", "tag")} for t in raw["threads"]],
                lines=[list(p) for p in sorted({(t["plant"], t["payoff"]) for t in raw["threads"]})],
                fixedReminders=fixed)


if __name__ == "__main__":
    p = argparse.ArgumentParser()
    p.add_argument("--chapters", type=int, default=50)
    p.add_argument("--seed", type=int, default=7)
    a = p.parse_args()
    try:
        print(json.dumps(generate(a.chapters, a.seed), ensure_ascii=False))
    except ValueError as e:
        p.error(str(e))
