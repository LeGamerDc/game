import fs from "node:fs";
import path from "node:path";

const outDir = path.dirname(new URL(import.meta.url).pathname);

const W = 1600;
const H = 900;
const C = {
  bg: "#f7f6f1",
  ink: "#24313a",
  muted: "#6e7b83",
  line: "#b9c4c8",
  teal: "#0f8b8d",
  teal2: "#d9f0ef",
  amber: "#e2a72e",
  amber2: "#fff0c2",
  red: "#c4514a",
  red2: "#f8dedb",
  green: "#4d9964",
  green2: "#dff1e4",
  card: "#ffffff",
};

function esc(s) {
  return String(s)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function svg(title, body) {
  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">
  <rect width="${W}" height="${H}" fill="${C.bg}"/>
  ${text(70, 86, title, "title")}
  ${body}
</svg>`;
}

function roundRect(x, y, w, h, fill = C.card, stroke = C.line, r = 18, sw = 3) {
  return `<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="${r}" fill="${fill}" stroke="${stroke}" stroke-width="${sw}"/>`;
}

function text(x, y, value, cls = "label", anchor = "start") {
  const styles = {
    title: { size: 44, weight: 700, fill: C.ink, family: "Arial, Helvetica, sans-serif" },
    subtitle: { size: 24, weight: 500, fill: C.muted, family: "Arial, Helvetica, sans-serif" },
    label: { size: 24, weight: 600, fill: C.ink, family: "Arial, Helvetica, sans-serif" },
    small: { size: 20, weight: 500, fill: C.muted, family: "Arial, Helvetica, sans-serif" },
    mono: { size: 21, weight: 600, fill: C.ink, family: "Menlo, Consolas, monospace" },
    tiny: { size: 17, weight: 500, fill: C.muted, family: "Arial, Helvetica, sans-serif" },
  };
  const s = styles[cls] ?? styles.label;
  return `<text x="${x}" y="${y}" text-anchor="${anchor}" font-family="${s.family}" font-size="${s.size}" font-weight="${s.weight}" fill="${s.fill}">${esc(value)}</text>`;
}

function line(x1, y1, x2, y2, stroke = C.line, sw = 4, dashed = false, arrow = true) {
  const dash = dashed ? ` stroke-dasharray="10 12"` : "";
  const marker = arrow ? ` marker-end="url(#arrow)"` : "";
  return `<line x1="${x1}" y1="${y1}" x2="${x2}" y2="${y2}" stroke="${stroke}" stroke-width="${sw}" stroke-linecap="round"${dash}${marker}/>`;
}

function defs() {
  return `<defs>
    <marker id="arrow" markerWidth="14" markerHeight="14" refX="10" refY="5" orient="auto" markerUnits="strokeWidth">
      <path d="M0,0 L10,5 L0,10 z" fill="${C.line}"/>
    </marker>
    <marker id="arrowTeal" markerWidth="14" markerHeight="14" refX="10" refY="5" orient="auto" markerUnits="strokeWidth">
      <path d="M0,0 L10,5 L0,10 z" fill="${C.teal}"/>
    </marker>
    <filter id="shadow" x="-20%" y="-20%" width="140%" height="140%">
      <feDropShadow dx="0" dy="8" stdDeviation="8" flood-color="#16333a" flood-opacity="0.11"/>
    </filter>
  </defs>`;
}

function node(x, y, label, fill = C.card, stroke = C.line, w = 190, h = 62) {
  return `<g filter="url(#shadow)">
    ${roundRect(x, y, w, h, fill, stroke, 14, 3)}
    ${text(x + w / 2, y + 39, label, "label", "middle")}
  </g>`;
}

function card(x, y, title, rows, color = C.teal, fill = C.teal2, w = 310) {
  return `<g filter="url(#shadow)">
    ${roundRect(x, y, w, 76 + rows.length * 38, C.card, color, 16, 3)}
    <rect x="${x}" y="${y}" width="${w}" height="56" rx="16" fill="${fill}"/>
    ${text(x + 24, y + 37, title, "label")}
    ${rows.map((r, i) => text(x + 30, y + 94 + i * 38, r, "small")).join("")}
  </g>`;
}

const arrowDefs = defs();

const figures = {
  "fig-root-tick-vs-stack.svg": svg(
    "Root Tick vs. Continuation Stack",
    `${arrowDefs}
    ${text(235, 150, "Repeated root traversal", "subtitle", "middle")}
    ${text(1115, 150, "Resume the active leaf", "subtitle", "middle")}
    ${roundRect(60, 190, 690, 590, "#ffffff", C.line, 24, 2)}
    ${roundRect(850, 190, 690, 590, "#ffffff", C.line, 24, 2)}
    ${["Tick 101", "Tick 102", "Tick 103"].map((t, i) => {
      const y = 265 + i * 150;
      return `${text(100, y + 42, t, "label")}
        ${node(245, y, "Root", C.card, C.line, 125, 56)}
        ${line(372, y + 28, 430, y + 28, C.line, 4)}
        ${node(445, y, "Selector", C.card, C.line, 170, 56)}
        ${line(617, y + 28, 675, y + 28, C.line, 4)}
        ${node(690, y, "WaitMove", C.amber2, C.amber, 190, 56)}`;
    }).join("")}
    ${text(405, 720, "Same path walked again and again", "small", "middle")}
    ${node(980, 270, "WaitMove frame", C.amber2, C.amber, 260, 64)}
    ${node(1000, 360, "Sequence frame", C.teal2, C.teal, 220, 60)}
    ${node(1020, 446, "Selector frame", C.card, C.line, 180, 60)}
    ${node(1040, 532, "Root frame", C.card, C.line, 140, 60)}
    ${line(1110, 334, 1110, 358, C.teal, 4, false, false)}
    ${line(1110, 420, 1110, 444, C.teal, 4, false, false)}
    ${line(1110, 506, 1110, 530, C.teal, 4, false, false)}
    ${text(1110, 650, "During waiting, only the top frame runs", "small", "middle")}`
  ),

  "fig-active-path-stack.svg": svg(
    "Active Path as a Runtime Continuation",
    `${arrowDefs}
    ${text(270, 155, "Static tree", "subtitle", "middle")}
    ${text(1130, 155, "Runtime stack", "subtitle", "middle")}
    ${node(190, 220, "Root", C.card, C.line, 180, 62)}
    ${node(155, 340, "Selector", C.teal2, C.teal, 250, 62)}
    ${node(155, 460, "Sequence", C.teal2, C.teal, 250, 62)}
    ${node(60, 595, "Guard", C.card, C.line, 180, 60)}
    ${node(290, 595, "WaitMove", C.amber2, C.amber, 210, 60)}
    ${line(280, 282, 280, 338, C.teal, 4)}
    ${line(280, 402, 280, 458, C.teal, 4)}
    ${line(280, 522, 155, 592, C.line, 4)}
    ${line(280, 522, 390, 592, C.teal, 5)}
    ${text(295, 710, "Highlighted path becomes the continuation", "small", "middle")}
    ${card(920, 230, "top", ["WaitMove frame", "next wake: +5"], C.amber, C.amber2, 420)}
    ${card(940, 390, "parent", ["Sequence frame", "child index: 1"], C.teal, C.teal2, 380)}
    ${card(960, 540, "parent", ["Selector frame", "child index: 0"], C.teal, C.teal2, 340)}
    ${card(980, 690, "parent", ["Root frame"], C.line, "#eef2f2", 300)}`
  ),

  "fig-resume-unwind.svg": svg(
    "Short-Path Resume and Parent Unwind",
    `${arrowDefs}
    ${card(90, 210, "T0 enter", ["push Selector", "push Sequence", "push WaitMove", "leaf returns +5"], C.teal, C.teal2, 405)}
    ${card(600, 210, "T5 resume", ["run WaitMove only", "leaf returns Success"], C.amber, C.amber2, 405)}
    ${card(1110, 210, "unwind", ["pop WaitMove", "Sequence sees Success", "push Attack"], C.green, C.green2, 405)}
    ${line(500, 390, 590, 390, C.line, 6)}
    ${line(1010, 390, 1100, 390, C.line, 6)}
    ${roundRect(120, 635, 1360, 86, "#ffffff", C.line, 18, 2)}
    ${text(800, 688, "The runtime pays the full path cost when entering or changing behavior, not while the leaf is waiting.", "label", "middle")}`
  ),

  "fig-guard-subroot.svg": svg(
    "Persistent Guard as a Scoped Sub-Root",
    `${arrowDefs}
    ${roundRect(95, 200, 575, 545, "#ffffff", C.line, 24, 2)}
    ${text(382, 255, "Outer continuation", "subtitle", "middle")}
    ${node(250, 350, "AlwaysGuard frame", C.teal2, C.teal, 300, 70)}
    ${text(383, 475, "guard: target still valid?", "label", "middle")}
    ${line(383, 420, 383, 455, C.teal, 5)}
    ${roundRect(820, 200, 680, 545, "#ffffff", C.line, 24, 2)}
    ${text(1160, 255, "Inner root resumes child behavior", "subtitle", "middle")}
    ${node(1000, 345, "MoveToTarget", C.amber2, C.amber, 300, 70)}
    ${node(1030, 470, "Chase sequence", C.teal2, C.teal, 240, 64)}
    ${node(1060, 590, "Selector", C.card, C.line, 180, 60)}
    ${line(552, 385, 990, 380, C.teal, 5)}
    ${line(1150, 416, 1150, 468, C.teal, 4)}
    ${line(1150, 535, 1150, 588, C.teal, 4)}
    ${text(800, 805, "Guard failure cancels the inner root; guard success resumes only the scoped child stack.", "label", "middle")}`
  ),

  "fig-parallel-roots.svg": svg(
    "Parallel Branches as Multiple Continuations",
    `${arrowDefs}
    ${roundRect(120, 175, 1360, 615, "#ffffff", C.line, 26, 2)}
    ${card(160, 250, "Join frame", ["require: 2 successes", "status: [running, running, success]", "cancel remaining roots when done"], C.teal, C.teal2, 455)}
    ${line(620, 365, 780, 260, C.teal, 5)}
    ${line(620, 400, 780, 430, C.teal, 5)}
    ${line(620, 435, 780, 600, C.teal, 5)}
    ${card(800, 215, "Child root A", ["top: WaitAnimation", "next wake: +3"], C.amber, C.amber2, 420)}
    ${card(800, 385, "Child root B", ["top: MoveToCover", "next wake: +1"], C.amber, C.amber2, 420)}
    ${card(800, 555, "Child root C", ["top: AimAtTarget", "status: Success"], C.green, C.green2, 420)}
    ${text(800, 845, "A parallel node does not need one multi-headed stack; it owns several child roots.", "label", "middle")}`
  ),

  "fig-event-wake.svg": svg(
    "Events and Scheduled Wakeups",
    `${arrowDefs}
    ${roundRect(80, 175, 650, 600, "#ffffff", C.line, 24, 2)}
    ${text(405, 240, "Event dispatch follows the active path", "subtitle", "middle")}
    ${node(245, 310, "Event arrives", C.red2, C.red, 250, 64)}
    ${line(370, 375, 370, 430, C.red, 5)}
    ${node(230, 445, "Root top frame", C.teal2, C.teal, 280, 64)}
    ${line(370, 510, 370, 575, C.teal, 5)}
    ${node(185, 590, "Leaf or sub-root handles it", C.amber2, C.amber, 370, 64)}
    ${roundRect(850, 175, 650, 600, "#ffffff", C.line, 24, 2)}
    ${text(1175, 240, "Deadline wake can be cut short", "subtitle", "middle")}
    ${line(930, 505, 1410, 505, C.line, 5, false, false)}
    <circle cx="930" cy="505" r="13" fill="${C.teal}"/>
    <circle cx="1140" cy="505" r="13" fill="${C.red}"/>
    <circle cx="1410" cy="505" r="13" fill="${C.amber}"/>
    ${text(930, 455, "0s", "label", "middle")}
    ${text(1140, 455, "2s", "label", "middle")}
    ${text(1410, 455, "5s", "label", "middle")}
    ${text(930, 565, "BT returns +5", "small", "middle")}
    ${text(1140, 565, "event completes task", "small", "middle")}
    ${text(1410, 565, "old wake skipped", "small", "middle")}
    ${text(1175, 680, "The tree sleeps until a deadline or a relevant event.", "label", "middle")}`
  ),

  "fig-cancel-unwind.svg": svg(
    "Cancellation as Stack Unwind",
    `${arrowDefs}
    ${roundRect(130, 180, 1340, 600, "#ffffff", C.line, 26, 2)}
    ${node(250, 315, "Cancel request", C.red2, C.red, 260, 70)}
    ${line(515, 350, 670, 350, C.red, 6)}
    ${card(710, 235, "top", ["WaitMove", "cleanup movement request"], C.amber, C.amber2, 470)}
    ${card(760, 405, "parent", ["ChaseSequence", "release local state"], C.teal, C.teal2, 420)}
    ${card(810, 575, "parent", ["AlwaysGuard", "cancel inner root"], C.red, C.red2, 370)}
    ${line(945, 395, 970, 405, C.line, 4, true)}
    ${line(970, 565, 995, 575, C.line, 4, true)}
    ${text(800, 835, "Normal completion and interruption both have one visible cleanup path: pop frames from top to root.", "label", "middle")}`
  ),
};

for (const [name, content] of Object.entries(figures)) {
  fs.writeFileSync(path.join(outDir, name), content);
}

console.log(`wrote ${Object.keys(figures).length} SVG figures to ${outDir}`);
