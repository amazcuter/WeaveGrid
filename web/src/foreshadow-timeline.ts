import type { Project } from "./types";

export type TimelineConnection = {
  id: string;
  label: string;
  plant: number;
  payoff: number;
  reminds: number[];
  added: boolean;
  startOffset: number;
  endOffset: number;
};

export function buildTimeline(project: Project) {
  const total = project.structure.chapters;
  const records = new Map(
    project.chapters.map((chapter) => [chapter.number, chapter]),
  );
  const turns = new Map(
    project.structure.nodes.map((node) => [node.ch, node.kind]),
  );
  const chapters = Array.from({ length: total }, (_, index) => {
    const number = index + 1;
    const record = records.get(number);
    return {
      number,
      outline: !!record?.outline,
      draft: !!record?.draft,
      turning: turns.get(number),
      plants: 0,
      payoffs: 0,
      reminders: 0,
    };
  });
  const threads = project.structure.threads?.length
    ? project.structure.threads
    : project.structure.lines.map(([plant, payoff], i) => ({
        id: `T${i + 1}`,
        plant,
        payoff,
        reminds: [],
      }));
  const connections: TimelineConnection[] = [
    ...threads.map((thread) => ({
      id: `thread:${thread.id}`,
      label: thread.id,
      plant: thread.plant,
      payoff: thread.payoff,
      reminds: [...new Set(thread.reminds || [])].filter(
        (ch) => ch > thread.plant && ch < thread.payoff,
      ),
      added: false,
      startOffset: 0,
      endOffset: 0,
    })),
    ...(project.links || [])
      .filter((link) => link.valid)
      .map((link) => ({
        id: `link:${link.id}`,
        label: link.reason || "新增关联",
        plant: link.from,
        payoff: link.to,
        reminds: [],
        added: true,
        startOffset: 0,
        endOffset: 0,
      })),
  ].filter((line) =>
    [line.plant, line.payoff].every(
      (ch) => Number.isInteger(ch) && ch >= 1 && ch <= total,
    ),
  );

  const starts = new Map<number, TimelineConnection[]>();
  const ends = new Map<number, TimelineConnection[]>();
  for (const line of connections) {
    starts.set(line.plant, [...(starts.get(line.plant) || []), line]);
    ends.set(line.payoff, [...(ends.get(line.payoff) || []), line]);
    if (line.added) continue;
    chapters[line.plant - 1].plants++;
    chapters[line.payoff - 1].payoffs++;
    for (const ch of line.reminds) chapters[ch - 1].reminders++;
  }
  // 同章出发、同章收回的独立伏笔也各占一个端点，不合并数量或覆盖连线。
  for (const [groups, offset] of [
    [starts, "startOffset"],
    [ends, "endOffset"],
  ] as const) {
    for (const group of groups.values()) {
      group.forEach((line, index) => {
        line[offset] =
          group.length === 1
            ? 0
            : ((index + 1) / (group.length + 1) - 0.5) * 0.65;
      });
    }
  }
  return {
    chapters,
    connections,
    outlineCount: chapters.filter((ch) => ch.outline).length,
    draftCount: chapters.filter((ch) => ch.draft).length,
    threadCount: connections.filter((line) => !line.added).length,
    maxDensity: Math.max(
      1,
      ...chapters.flatMap((ch) => [ch.plants, ch.payoffs]),
    ),
    maxActivity: Math.max(
      1,
      ...chapters.map((ch) => ch.plants + ch.payoffs + ch.reminders),
    ),
  };
}

export const TIMELINE_PADDING = 24;
export function chapterX(chapter: number, spacing: number) {
  return TIMELINE_PADDING + (chapter - 0.5) * spacing;
}

export function connectionGeometry(
  line: TimelineConnection,
  spacing: number,
  top: number,
  bottom: number,
) {
  const x1 = chapterX(line.plant, spacing) + line.startOffset * spacing;
  const x2 = chapterX(line.payoff, spacing) + line.endOffset * spacing;
  const y1 = top + (bottom - top) * 0.48;
  const y2 = bottom - (bottom - top) * 0.48;
  const path = `M ${x1} ${top} C ${x1} ${y1}, ${x2} ${y2}, ${x2} ${bottom}`;
  const reminders = line.reminds.map((chapter) => {
    const x = chapterX(chapter, spacing);
    const fraction = Math.max(0, Math.min(1, (x - x1) / (x2 - x1)));
    // 反解贝塞尔曲线的横坐标，让提醒点严格对齐所在章节刻度。
    let lo = 0,
      hi = 1;
    for (let i = 0; i < 30; i++) {
      const t = (lo + hi) / 2;
      if (t * t * (3 - 2 * t) < fraction) lo = t;
      else hi = t;
    }
    const t = (lo + hi) / 2,
      u = 1 - t;
    return {
      chapter,
      x,
      y:
        u ** 3 * top +
        3 * u * u * t * y1 +
        3 * u * t * t * y2 +
        t ** 3 * bottom,
    };
  });
  return { path, x1, x2, reminders };
}

export function visibleChapterRange(
  scroll: number,
  width: number,
  spacing: number,
  total: number,
) {
  return {
    first: Math.max(
      1,
      Math.min(total, Math.floor((scroll - TIMELINE_PADDING) / spacing) + 1),
    ),
    last: Math.max(
      1,
      Math.min(total, Math.ceil((scroll + width - TIMELINE_PADDING) / spacing)),
    ),
  };
}
