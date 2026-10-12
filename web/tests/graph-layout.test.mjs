import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  compactPositions,
  routeConnections,
  segmentsOverlap,
  crossesBox,
  parseLayout,
} from "../src/graph-layout.ts";

const definitions = JSON.parse(
  readFileSync(
    new URL("../../internal/workbench/flow_edges.json", import.meta.url),
  ),
);
function checkRoutes(ids, edges, positions = compactPositions(ids)) {
  const boxes = ids.map((id) => ({
    id,
    ...positions[id],
    width: 196,
    height: 88,
  }));
  const routes = routeConnections(boxes, edges);
  assert.equal(Object.keys(routes).length, edges.length);
  const segments = [];
  for (const edge of edges) {
    const points = routes[edge.id].points;
    assert.ok(points.length >= 2);
    for (let i = 1; i < points.length; i++) {
      const a = points[i - 1],
        b = points[i];
      assert.ok(a.x === b.x || a.y === b.y, `斜线 ${edge.id}`);
      for (const old of segments) {
        assert.equal(
          segmentsOverlap(a, b, old.a, old.b),
          false,
          `${edge.id} 与 ${old.id} 重叠: ${JSON.stringify([a, b, old.a, old.b])}`,
        );
      }
      segments.push({ a, b, id: edge.id });
      for (const box of boxes) {
        assert.equal(
          crossesBox(a, b, box),
          false,
          `${edge.id} 穿过 ${box.id}: ${JSON.stringify([a, b])}`,
        );
      }
    }
  }
}
test("全部子流程的分支和返回路径都是直角且不穿过节点", () => {
  for (const [name, list] of Object.entries(definitions)) {
    const ids = [
      ...new Set(list.flatMap(([a, b]) => [name + a, name + b])),
    ].sort((a, b) => Number(a.slice(1)) - Number(b.slice(1)));
    checkRoutes(
      ids,
      list.map(([a, b]) => ({
        id: `${name}${a}-${b}`,
        source: name + a,
        target: name + b,
        feedback: Number(a) > Number(b),
      })),
    );
  }
});
test("两个主流程包含完成出口，拖动节点后重新避让", () => {
  for (const ids of [
    ["A", "B", "C", "D", "F", "G", "J", "H", "done"],
    ["A", "B", "C", "E", "F", "G", "J", "H", "done"],
  ]) {
    const path = ids.filter((id) => id !== "H" && id !== "done");
    const edges = path.slice(1).map((id, i) => ({
      id: path[i] + "-" + id,
      source: path[i],
      target: id,
    }));
    edges.push(
      ...[
        ["J", "H"],
        ["J", "done"],
        ["J", "B"],
        ["H", "F"],
        ["H", "J"],
      ].map(([source, target], i) => ({
        id: source + "-" + target,
        source,
        target,
        feedback: i > 1,
      })),
    );
    checkRoutes(ids, edges);
    const positions = compactPositions(ids);
    positions.B = { x: 380, y: -120 };
    positions.H = { x: 920, y: 400 };
    checkRoutes(ids, edges, positions);
  }
});
test("布局可往返保存，拒绝错误子流、缺失节点和异常坐标", () => {
  const value = {
    format: "novelflow-layout",
    version: 1,
    graph: "backward:main",
    direction: "compact",
    positions: compactPositions(["A", "B"]),
  };
  assert.deepEqual(
    parseLayout(JSON.stringify(value), value.graph, ["A", "B"]),
    value,
  );
  assert.throws(() =>
    parseLayout(JSON.stringify(value), "forward:main", ["A", "B"]),
  );
  assert.throws(() =>
    parseLayout(JSON.stringify(value), value.graph, ["A", "B", "C"]),
  );
  value.positions.A.x = Infinity;
  assert.throws(() =>
    parseLayout(JSON.stringify(value), value.graph, ["A", "B"]),
  );
});

test("完整大图分组后仍无重叠线，移动整组和子步骤后保持关系", async () => {
  const { routeGroupedConnections } = await import("../src/graph-layout.ts");
  const groups = Object.entries(definitions).map(([id, list], i) => {
    const ids = [...new Set(list.flatMap(([a, b]) => [id + a, id + b]))].sort(
      (a, b) => Number(a.slice(1)) - Number(b.slice(1)),
    );
    return {
      id,
      ids,
      x: (i % 3) * 1150,
      y: Math.floor(i / 3) * 1100,
      width: 936,
      height: Math.ceil(ids.length / 3) * 184 + 120,
    };
  });
  const boxes = groups.map(({ ids, ...box }) => box);
  for (const group of groups) {
    const positions = compactPositions(group.ids);
    for (const id of group.ids)
      boxes.push({
        id,
        parentId: group.id,
        x: positions[id].x + 80,
        y: positions[id].y + 112,
        width: 196,
        height: 88,
      });
  }
  boxes.push({ id: "done", x: 1150, y: 3300, width: 196, height: 88 });
  const edges = Object.entries(definitions).flatMap(([id, list]) =>
    list.map(([a, b]) => ({
      id: id + a + "-" + id + b,
      source: id + a,
      target: id + b,
      feedback: Number(b) < Number(a),
    })),
  );
  edges.push(
    ...[
      ["A", "J"],
      ["B", "C"],
      ["C", "D"],
      ["C", "E"],
      ["D", "F"],
      ["E", "F"],
      ["F", "G"],
      ["G", "J"],
      ["J", "H"],
      ["J", "done"],
      ["J", "B"],
      ["H", "F"],
      ["H", "J"],
    ].map(([source, target], i) => ({
      id: source + "-" + target,
      source,
      target,
      feedback: i >= 12,
    })),
  );
  for (const moved of [false, true]) {
    if (moved) {
      boxes.find((b) => b.id === "B").x += 120;
      boxes.find((b) => b.id === "B").y -= 144;
      boxes.find((b) => b.id === "B7").x += 24;
      boxes.find((b) => b.id === "B7").y -= 24;
    }
    const routes = routeGroupedConnections(boxes, edges),
      segments = [];
    assert.equal(Object.keys(routes).length, edges.length);
    for (const [id, route] of Object.entries(routes))
      for (let i = 1; i < route.points.length; i++) {
        const a = route.points[i - 1],
          b = route.points[i];
        assert.ok(a.x === b.x || a.y === b.y);
        for (const old of segments)
          assert.equal(
            segmentsOverlap(a, b, old.a, old.b),
            false,
            `${id} 与 ${old.id} 重叠`,
          );
        segments.push({ id, a, b });
      }
  }
});

test("50 章关系图保留全部独立连线且不共用线段", () => {
  const ids = Array.from({ length: 50 }, (_, i) => String(i + 1));
  const lines = JSON.parse(
    readFileSync(
      new URL(
        "../../internal/workbench/testdata/lines50.json",
        import.meta.url,
      ),
    ),
  );
  checkRoutes(
    ids,
    lines.map(([a, b]) => ({
      id: `${a}-${b}`,
      source: String(a),
      target: String(b),
    })),
    compactPositions(ids, 6),
  );
});

test("移除文风子流后保留用户其他节点的位置", () => {
  const original = {
    format: "novelflow-layout",
    version: 1,
    graph: "workflow:all",
    direction: "compact",
    positions: {
      A: { x: 144, y: 288 },
      C2: { x: 96, y: 112 },
      I: { x: 450, y: 500 },
      I4: { x: 22, y: 40 },
    },
  };
  const migrated = parseLayout(JSON.stringify(original), "workflow:all", [
    "A",
    "C2",
  ]);
  assert.deepEqual(migrated.positions, {
    A: original.positions.A,
    C2: original.positions.C2,
  });
  original.positions.unknown = { x: 0, y: 0 };
  assert.throws(() =>
    parseLayout(JSON.stringify(original), "workflow:all", ["A", "C2"]),
  );
});
