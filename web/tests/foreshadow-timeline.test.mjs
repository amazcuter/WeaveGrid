import { test } from "node:test";
import assert from "node:assert/strict";
import {
  buildTimeline,
  chapterX,
  connectionGeometry,
  visibleChapterRange,
} from "../src/foreshadow-timeline.ts";

function project(chapters = 50) {
  return {
    structure: {
      chapters,
      nodes: [{ ch: 10, kind: "转折" }],
      arcs: [],
      lines: [[1, 10]],
      threads: [
        { id: "T1", plant: 1, payoff: 10, reminds: [4, 7] },
        { id: "T2", plant: 1, payoff: 10, reminds: [4] },
        { id: "T3", plant: 10, payoff: 50, reminds: [25] },
      ],
    },
    chapters: Array.from({ length: chapters }, (_, i) => ({
      number: i + 1,
      outline: i >= 39 ? `outline-${i}` : "",
      draft: i < 3 ? `draft-${i}` : "",
    })),
    links: [],
  };
}

test("倒序大纲、正序正文按各章实际版本标记，不把中间空白算作完成", () => {
  const p = project();
  p.chapters[44].outline = "";
  p.chapters.reverse();
  const model = buildTimeline(p);
  assert.equal(model.outlineCount, 10);
  assert.equal(model.draftCount, 3);
  assert.equal(model.chapters[0].outline, false);
  assert.equal(model.chapters[39].outline, true);
  assert.equal(model.chapters[44].outline, false);
  assert.equal(model.chapters[49].outline, true);
  assert.equal(model.chapters[2].draft, true);
  assert.equal(model.chapters[3].draft, false);
});

test("重复起止的独立伏笔分别计数和走线，提醒也按真实章号累计", () => {
  const model = buildTimeline(project());
  assert.equal(model.threadCount, 3);
  assert.equal(model.chapters[0].plants, 2);
  assert.equal(model.chapters[9].payoffs, 2);
  assert.equal(model.chapters[9].plants, 1);
  assert.equal(model.chapters[3].reminders, 2);
  assert.equal(model.chapters[9].turning, "转折");
  assert.equal(
    model.chapters[4].plants +
      model.chapters[4].payoffs +
      model.chapters[4].reminders,
    0,
  );
  const [a, b] = model.connections.map((line) =>
    connectionGeometry(line, 56, 192, 450),
  );
  assert.notEqual(a.path, b.path);
  assert.notEqual(a.x1, b.x1);
  assert.notEqual(a.x2, b.x2);
  for (const point of a.reminders) {
    assert.equal(point.x, chapterX(point.chapter, 56));
    assert.ok(point.y > 192 && point.y < 450);
  }
  assert.ok(a.reminders[0].y < a.reminders[1].y);
});

test("新增有效关联保留方向且不计入伏笔密度，忽略失效和越界关联", () => {
  const p = project();
  p.links = [
    { id: "back", from: 40, to: 2, valid: true, reason: "补充呼应" },
    { id: "old", from: 1, to: 5, valid: false },
    { id: "outside", from: 4, to: 51, valid: true },
  ];
  const model = buildTimeline(p);
  assert.equal(model.connections.length, 4);
  assert.equal(model.threadCount, 3);
  const link = model.connections.find((line) => line.added);
  assert.equal(link.plant, 40);
  assert.equal(link.payoff, 2);
  assert.equal(model.chapters[39].plants, 0);
  assert.equal(model.chapters[1].payoffs, 0);
});

test("空结构、旧版关系导入和 2000 章都保留每个章节刻度", () => {
  const p = project(1);
  p.structure.threads = [];
  p.structure.lines = [];
  let model = buildTimeline(p);
  assert.equal(model.chapters.length, 1);
  assert.equal(model.threadCount, 0);
  assert.equal(model.maxDensity, 1);
  p.structure.chapters = 2000;
  p.structure.lines = [[1, 2000]];
  model = buildTimeline(p);
  assert.equal(model.chapters.length, 2000);
  assert.equal(model.threadCount, 1);
  assert.equal(model.chapters[1999].payoffs, 1);
  assert.equal(chapterX(2000, 56) - chapterX(1, 56), 1999 * 56);
  assert.deepEqual(visibleChapterRange(0, 560, 56, 2000), {
    first: 1,
    last: 10,
  });
  assert.equal(visibleChapterRange(2000 * 56 - 500, 560, 56, 2000).last, 2000);
});
