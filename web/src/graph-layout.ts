// 布局只保存显示位置；节点与连线始终来自流程定义，不改变执行顺序。
export type Point = { x: number; y: number };
export type Direction = "compact" | "RIGHT" | "DOWN";
export type Box = Point & { id: string; width: number; height: number };
export type Side = "left" | "right" | "top" | "bottom";
export type Connection = {
  id: string;
  source: string;
  target: string;
  feedback?: boolean;
  label?: unknown;
};
export type Route = {
  points: Point[];
  sourceSide: Side;
  targetSide: Side;
  sourceOffset: number;
  targetOffset: number;
  label: Point;
};
export type LayoutFile = {
  format: "novelflow-layout";
  version: 1;
  graph: string;
  direction: Direction;
  positions: Record<string, Point>;
};

export function parseLayout(
  text: string,
  graph: string,
  ids: string[],
): LayoutFile {
  const value = JSON.parse(text);
  if (
    value?.format !== "novelflow-layout" ||
    value.version !== 1 ||
    value.graph !== graph
  )
    throw new Error("这份布局不属于当前流程，请切换到对应模式和子流程后导入。");
  if (
    !["compact", "RIGHT", "DOWN"].includes(value.direction) ||
    !value.positions ||
    typeof value.positions !== "object"
  )
    throw new Error("布局格式无效。");
  const positions: Record<string, Point> = {};
  for (const id of ids) {
    const p = value.positions[id];
    if (
      !p ||
      !Number.isFinite(p.x) ||
      !Number.isFinite(p.y) ||
      Math.abs(p.x) > 100000 ||
      Math.abs(p.y) > 100000
    )
      throw new Error(`节点 ${id} 的位置缺失或超出范围。`);
    positions[id] = { x: p.x, y: p.y };
  }
  // 仅移除已退役的文风节点，保留其余手工位置；其他拓扑差异仍明确拒绝。
  const extras = Object.keys(value.positions).filter((id) => !ids.includes(id));
  if (
    extras.length &&
    !(
      graph === "workflow:all" &&
      !ids.includes("I") &&
      extras.every((id) => /^I(?:[1-9])?$/.test(id))
    )
  )
    throw new Error("节点数量已变化，请重新自动排布。");
  return {
    format: "novelflow-layout",
    version: 1,
    graph,
    direction: value.direction,
    positions,
  };
}

export function compactPositions(
  ids: string[],
  columns = 3,
): Record<string, Point> {
  return Object.fromEntries(
    ids.map((id, i) => {
      const row = Math.floor(i / columns),
        column = i % columns;
      return [
        id,
        { x: (row % 2 ? columns - 1 - column : column) * 290, y: row * 184 },
      ];
    }),
  );
}

export function port(box: Box, side: Side, offset = 0.5): Point {
  if (side === "left") return { x: box.x, y: box.y + box.height * offset };
  if (side === "right")
    return { x: box.x + box.width, y: box.y + box.height * offset };
  if (side === "top") return { x: box.x + box.width * offset, y: box.y };
  return { x: box.x + box.width * offset, y: box.y + box.height };
}
function outside(p: Point, side: Side): Point {
  return {
    x: p.x + (side === "right" ? 24 : side === "left" ? -24 : 0),
    y: p.y + (side === "bottom" ? 24 : side === "top" ? -24 : 0),
  };
}
export function crossesBox(a: Point, b: Point, box: Box, margin = 0): boolean {
  const l = box.x - margin,
    r = box.x + box.width + margin,
    t = box.y - margin,
    d = box.y + box.height + margin;
  return a.x === b.x
    ? a.x > l && a.x < r && Math.max(a.y, b.y) > t && Math.min(a.y, b.y) < d
    : a.y > t && a.y < d && Math.max(a.x, b.x) > l && Math.min(a.x, b.x) < r;
}

// A* 在节点外侧的直角网格中找路，拖动后重新计算，避免连线穿过中间节点。
// 将移动方向纳入状态并惩罚转弯，优先选择短而少折返的路径。
function orthogonal(
  start: Point,
  end: Point,
  boxes: Box[],
  used: [Point, Point][],
): Point[] {
  const xs = [
    ...new Set([
      start.x,
      end.x,
      ...boxes.flatMap((b) => [
        b.x - 24,
        b.x - 40,
        b.x + b.width + 24,
        b.x + b.width + 40,
      ]),
      ...used
        .filter(([a, b]) => a.x === b.x)
        .flatMap(([a]) => [a.x - 12, a.x + 12]),
    ]),
  ].sort((a, b) => a - b);
  const ys = [
    ...new Set([
      start.y,
      end.y,
      ...boxes.flatMap((b) => [
        b.y - 24,
        b.y - 40,
        b.y + b.height + 24,
        b.y + b.height + 40,
      ]),
      ...used
        .filter(([a, b]) => a.y === b.y)
        .flatMap(([a]) => [a.y - 12, a.y + 12]),
    ]),
  ].sort((a, b) => a - b);
  const width = xs.length,
    origin = ys.indexOf(start.y) * width + xs.indexOf(start.x),
    goal = ys.indexOf(end.y) * width + xs.indexOf(end.x);
  const point = (i: number): Point => ({
    x: xs[i % width],
    y: ys[Math.floor(i / width)],
  });
  const distance = (a: Point, b: Point) =>
    Math.abs(a.x - b.x) + Math.abs(a.y - b.y);
  const heap: { key: number; score: number; cost: number }[] = [];
  const push = (entry: (typeof heap)[number]) => {
    heap.push(entry);
    let i = heap.length - 1;
    while (i > 0) {
      const parent = (i - 1) >> 1;
      if (heap[parent].score <= entry.score) break;
      heap[i] = heap[parent];
      i = parent;
    }
    heap[i] = entry;
  };
  const pop = () => {
    const head = heap[0],
      tail = heap.pop()!;
    if (heap.length) {
      let i = 0;
      while (2 * i + 1 < heap.length) {
        let next = 2 * i + 1;
        if (next + 1 < heap.length && heap[next + 1].score < heap[next].score)
          next++;
        if (tail.score <= heap[next].score) break;
        heap[i] = heap[next];
        i = next;
      }
      heap[i] = tail;
    }
    return head;
  };
  const best = new Map<number, number>(),
    previous = new Map<number, number>();
  const originKey = origin * 3 + 2;
  best.set(originKey, 0);
  push({ key: originKey, cost: 0, score: distance(start, end) });
  const blocked = new Map<string, boolean>();
  while (heap.length) {
    const item = pop(),
      index = Math.floor(item.key / 3),
      direction = item.key % 3;
    if (item.cost !== best.get(item.key)) continue;
    if (index === goal) {
      const result: Point[] = [];
      let key: number | undefined = item.key;
      while (key !== undefined) {
        result.push(point(Math.floor(key / 3)));
        key = previous.get(key);
      }
      return result.reverse();
    }
    const col = index % width,
      row = Math.floor(index / width),
      a = point(index);
    const neighbors = [
      col > 0 ? index - 1 : -1,
      col + 1 < width ? index + 1 : -1,
      row > 0 ? index - width : -1,
      row + 1 < ys.length ? index + width : -1,
    ];
    for (const next of neighbors) {
      if (next < 0) continue;
      const b = point(next),
        segment = `${Math.min(index, next)}:${Math.max(index, next)}`;
      if (!blocked.has(segment))
        blocked.set(
          segment,
          boxes.some((box) => crossesBox(a, b, box, 12)),
        );
      if (blocked.get(segment)) continue;
      const axis = a.x === b.x ? 1 : 0,
        key = next * 3 + axis;
      // 不允许复用任何已有线段。交叉可以穿越，平行线必须使用独立通道。
      if (used.some(([u, v]) => segmentsOverlap(a, b, u, v))) continue;
      const cost =
        item.cost +
        distance(a, b) +
        (direction !== 2 && direction !== axis ? 30 : 0);
      if (cost >= (best.get(key) ?? Infinity)) continue;
      best.set(key, cost);
      previous.set(key, item.key);
      push({ key, cost, score: cost + distance(b, end) });
    }
  }
  // 用户把卡片叠在一起时可能无可通行路径；仍保留直角连线，移开卡片后恢复避让。
  return [start, { x: end.x, y: start.y }, end];
}

export function segmentsOverlap(
  a: Point,
  b: Point,
  u: Point,
  v: Point,
): boolean {
  return a.x === b.x && u.x === v.x && a.x === u.x
    ? Math.min(Math.max(a.y, b.y), Math.max(u.y, v.y)) >
        Math.max(Math.min(a.y, b.y), Math.min(u.y, v.y))
    : a.y === b.y &&
        u.y === v.y &&
        a.y === u.y &&
        Math.min(Math.max(a.x, b.x), Math.max(u.x, v.x)) >
          Math.max(Math.min(a.x, b.x), Math.min(u.x, v.x));
}

export function routeConnections(
  boxes: Box[],
  edges: Connection[],
): Record<string, Route> {
  const lookup = new Map(boxes.map((b) => [b.id, b])),
    result: Record<string, Route> = {},
    used: [Point, Point][] = [];
  const prepared: {
    edge: Connection;
    a: Box;
    b: Box;
    sourceSide: Side;
    targetSide: Side;
    sourceOffset: number;
    targetOffset: number;
  }[] = [];
  for (const edge of [...edges].sort(
    (a, b) => Number(!!a.feedback) - Number(!!b.feedback),
  )) {
    const a = lookup.get(edge.source),
      b = lookup.get(edge.target);
    if (!a || !b) continue;
    const dx = b.x + b.width / 2 - a.x - a.width / 2,
      dy = b.y + b.height / 2 - a.y - a.height / 2;
    let sourceSide: Side, targetSide: Side;
    if (Math.abs(dx) > Math.abs(dy)) {
      sourceSide = dx > 0 ? "right" : "left";
      targetSide = dx > 0 ? "left" : "right";
    } else {
      sourceSide = dy > 0 ? "bottom" : "top";
      targetSide = dy > 0 ? "top" : "bottom";
    }
    if (edge.feedback) {
      if (Math.abs(dx) > Math.abs(dy)) sourceSide = targetSide = "bottom";
      else sourceSide = targetSide = "left";
    }
    prepared.push({
      edge,
      a,
      b,
      sourceSide,
      targetSide,
      sourceOffset: 0.5,
      targetOffset: 0.5,
    });
  }
  // 同一侧的入线和出线也分配不同端口，杜绝“共用一截线”的模糊分叉。
  const groups = new Map<
    string,
    { item: (typeof prepared)[number]; source: boolean; order: number }[]
  >();
  for (const item of prepared) {
    for (const source of [true, false]) {
      const box = source ? item.a : item.b,
        other = source ? item.b : item.a,
        side = source ? item.sourceSide : item.targetSide;
      const key = box.id + ":" + side,
        group = groups.get(key) || [];
      group.push({
        item,
        source,
        order: side === "left" || side === "right" ? other.y : other.x,
      });
      groups.set(key, group);
    }
  }
  for (const group of groups.values()) {
    group.sort((a, b) => a.order - b.order);
    group.forEach(({ item, source }, i) => {
      const box = source ? item.a : item.b,
        side = source ? item.sourceSide : item.targetSide;
      const length =
        side === "left" || side === "right" ? box.height : box.width;
      const offset =
        Math.round((length * (i + 1)) / (group.length + 1)) / length;
      if (source) item.sourceOffset = offset;
      else item.targetOffset = offset;
    });
  }
  // 先预留全部端口短线，后画的边不会占用其他边进出节点的通道。
  const stubs: [Point, Point][] = prepared.flatMap((item) => {
    const start = port(item.a, item.sourceSide, item.sourceOffset),
      end = port(item.b, item.targetSide, item.targetOffset);
    return [
      [start, outside(start, item.sourceSide)],
      [end, outside(end, item.targetSide)],
    ] as [Point, Point][];
  });
  for (const item of prepared) {
    const { edge, a, b, sourceSide, targetSide, sourceOffset, targetOffset } =
      item;
    const start = port(a, sourceSide, sourceOffset),
      end = port(b, targetSide, targetOffset);
    const raw = [
      start,
      ...orthogonal(
        outside(start, sourceSide),
        outside(end, targetSide),
        boxes,
        [...used, ...stubs],
      ),
      end,
    ];
    const points: Point[] = [];
    for (const p of raw) {
      const last = points.at(-1),
        before = points.at(-2);
      if (last?.x === p.x && last.y === p.y) continue;
      if (
        before &&
        last &&
        ((before.x === last.x && last.x === p.x) ||
          (before.y === last.y && last.y === p.y))
      )
        points.pop();
      points.push(p);
    }
    let length = -Infinity,
      label = start;
    for (let i = 1; i < points.length; i++) {
      const u = points[i - 1],
        v = points[i];
      used.push([u, v]);
      const span = Math.abs(u.x - v.x) + Math.abs(u.y - v.y);
      const center = { x: (u.x + v.x) / 2, y: (u.y + v.y) / 2 };
      const labelWidth = String(edge.label || "").length * 12 + 14;
      const overlaps = boxes.some(
        (box) =>
          center.x + labelWidth / 2 > box.x - 5 &&
          center.x - labelWidth / 2 < box.x + box.width + 5 &&
          center.y + 12 > box.y &&
          center.y - 12 < box.y + box.height,
      );
      // 标签优先放在足够长的横线中部，避开节点，防止文字被卡片遮住。
      const score =
        span +
        (u.y === v.y && span >= labelWidth ? 1000 : 0) -
        (overlaps ? 10000 : 0);
      if (score > length) {
        length = score;
        label = center;
      }
    }
    result[edge.id] = {
      points,
      sourceSide,
      targetSide,
      sourceOffset,
      targetOffset,
      label,
    };
  }
  return result;
}

// 子流程内部使用自己的坐标和走线通道，组间连线只在分组边界外寻路。
// 移动整组只平移内部路径，不会改变子步骤之间的相对位置。
export function routeGroupedConnections(
  boxes: (Box & { parentId?: string })[],
  edges: Connection[],
): Record<string, Route> {
  const lookup = new Map(boxes.map((box) => [box.id, box]));
  const groups = new Map<string, Connection[]>();
  for (const edge of edges) {
    const parent = lookup.get(edge.source)?.parentId || "";
    const list = groups.get(parent) || [];
    list.push(edge);
    groups.set(parent, list);
  }
  const result: Record<string, Route> = {};
  for (const [parent, connections] of groups) {
    const local = routeConnections(
      boxes.filter((box) => (box.parentId || "") === parent),
      connections,
    );
    const origin = lookup.get(parent) || { x: 0, y: 0 };
    for (const [id, route] of Object.entries(local))
      result[id] = {
        ...route,
        points: route.points.map((p) => ({
          x: p.x + origin.x,
          y: p.y + origin.y,
        })),
        label: { x: route.label.x + origin.x, y: route.label.y + origin.y },
      };
  }
  return result;
}
