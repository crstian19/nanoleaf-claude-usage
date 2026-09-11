"use strict";

// The session code is the first segment of this page's own path, so every
// request is addressed the same way the page was.
const api = location.pathname.replace(/\/+$/, "");
const SVG_NS = "http://www.w3.org/2000/svg";

const el = (id) => document.getElementById(id);
// The drawing is #shape and the picker is #arrangement. They were both called
// "shape" once, and getElementById returned the SVG for both: the picker's
// replaceChildren() emptied the drawing and its options went inside it.
const svg = el("shape");
const dial = el("dial");
const groups = {
  glow: el("glow"),
  panels: el("panels"),
  spots: el("spots"),
  labels: el("labels"),
};

const state = { shape: "", rotation: 0, mode: "pattern", level: 0.6, phase: "idle" };

// The wall being built, when the page is building one.
const editor = {
  // shapes is the palette, kinds is what the server calls them, and armed is
  // the kind waiting to be dropped.
  kinds: [],
  name: "",
  armed: null,
  spots: [],
  nearest: null,
};

let extent = 0;
let drawn = new Map(); // panel id -> its three elements
let shownRotation = 0; // the rotation the drawing on screen was computed at
let drag = null;
let stream = null;
let live = true;

const wrap = (deg) => ((Math.round(deg) % 360) + 360) % 360;

// shortest turns an angle difference into the way round that is nearest, so
// going from 359 to 0 does not spin the shape the long way for one frame.
const shortest = (deg) => (((deg % 360) + 540) % 360) - 180;

function setStatus(text, kind) {
  const node = el("status");
  node.textContent = text;
  node.className = "status" + (kind ? " " + kind : "");
}

// stop disables the controls. Used when the session is over, either because
// the user said so or because nanoclaude is gone: a page that still moves
// after the panels have been handed back is a page that lies.
function stop(text, kind) {
  live = false;
  if (stream) stream.close();
  setStatus(text, kind);
  for (const control of document.querySelectorAll("button, input, select")) control.disabled = true;
  dial.style.cursor = "default";
}

// -- talking to nanoclaude ---------------------------------------------------

// send keeps one request in flight and remembers only the latest change, so
// dragging the shape cannot build a queue of stale angles.
let inflight = false;
let pending = null;

async function send(patch) {
  pending = Object.assign(pending || {}, patch);
  if (inflight) return;
  inflight = true;
  try {
    while (pending && live) {
      const body = pending;
      pending = null;
      const res = await post("state", body);
      if (!res) {
        // The change never reached the panels, so the drawing must not
        // pretend it did: fall back to the angle the wall is actually at.
        // A page showing one angle while the wall shows another is the one
        // thing this tool cannot do.
        state.rotation = shownRotation;
        showRotation();
        return;
      }
      adopt(await res.json());
    }
  } finally {
    inflight = false;
  }
}

// adopt takes what nanoclaude accepted, which is how a rotation it wrapped or
// a level it clamped reaches the controls. Skipped while the user is still
// moving something, since by then their value is the newer one.
function adopt(accepted) {
  if (pending || drag) return;
  if (typeof accepted.rotation === "number" && accepted.rotation !== state.rotation) {
    state.rotation = accepted.rotation;
    showRotation();
  }
}

async function post(path, body) {
  try {
    const res = await fetch(`${api}/${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      setStatus(`nanoclaude refused that: ${(await res.text()).trim()}`, "bad");
      return null;
    }
    return res;
  } catch {
    setStatus("nanoclaude is not answering. Is it still running?", "bad");
    return null;
  }
}

// -- drawing ----------------------------------------------------------------

function build(panels) {
  for (const group of Object.values(groups)) group.replaceChildren();
  drawn = new Map();

  for (const panel of panels) {
    const fill = document.createElementNS(SVG_NS, "polygon");
    const glow = document.createElementNS(SVG_NS, "polygon");
    const label = document.createElementNS(SVG_NS, "text");
    groups.glow.append(glow);
    groups.panels.append(fill);
    groups.labels.append(label);
    drawn.set(panel.id, { fill, glow, label });
  }
}

function draw(snap) {
  if (snap.extent !== extent) {
    extent = snap.extent;
    svg.setAttribute("viewBox", `${-extent} ${-extent} ${extent * 2} ${extent * 2}`);
  }

  const panels = snap.panels || [];
  const ids = panels.map((p) => p.id);
  if (ids.length !== drawn.size || ids.some((id) => !drawn.has(id))) build(panels);

  for (const panel of panels) {
    const parts = drawn.get(panel.id);
    if (!parts) continue;
    parts.fill.dataset.panel = String(panel.id);
    parts.fill.classList.toggle("removable", el("remove-tool").checked);
    parts.fill.setAttribute("points", panel.points);
    parts.fill.setAttribute("fill", panel.color);
    parts.glow.setAttribute("points", panel.points);
    parts.glow.setAttribute("fill", panel.color);
    // Every coordinate comes from nanoclaude, this one included: the page
    // does no geometry of its own beyond reading the mouse.
    parts.label.setAttribute("x", panel.label[0]);
    parts.label.setAttribute("y", panel.label[1]);
    const order = String(panel.order + 1);
    if (parts.label.textContent !== order) parts.label.textContent = order;
  }

  drawSpots(snap.spots || []);

  // An empty wall is a black square with nothing to explain it.
  el("empty-hint").textContent =
    snap.editing && panels.length === 0 ? "Drag a panel here to start" : "";

  const trouble = el("trouble");
  if (snap.trouble) {
    trouble.textContent = `The panels are refusing frames: ${snap.trouble}`;
    trouble.hidden = false;
  } else {
    trouble.hidden = true;
  }

  shownRotation = snap.rotation;
  turnDrawing();
}

// drawSpots shows every place the panel being dragged could land.
//
// The outlines come from nanoclaude, computed with the same geometry that
// will place the panel, so the shape under the cursor is the shape that lands.
function drawSpots(spots) {
  editor.spots = spots;
  if (spots.length !== groups.spots.childElementCount) {
    groups.spots.replaceChildren();
    for (const _ of spots) {
      groups.spots.append(document.createElementNS(SVG_NS, "polygon"));
    }
  }
  spots.forEach((spot, i) => {
    const node = groups.spots.children[i];
    node.setAttribute("points", spot.points);
    node.classList.toggle("near", editor.nearest === i);
  });
}

// markNearest highlights the spot the pointer is over.
function markNearest(event) {
  if (!editor.spots.length) return;

  const at = inDrawing(event);
  let best = null;
  let bestGap = Infinity;
  editor.spots.forEach((spot, i) => {
    const gap = Math.hypot(spot.x - at.x, spot.y - at.y);
    if (gap < bestGap) {
      best = i;
      bestGap = gap;
    }
  });
  editor.nearest = best;
  for (const [i, node] of [...groups.spots.children].entries()) {
    node.classList.toggle("near", i === best);
  }
}

// inDrawing converts a pointer position into the drawing's own coordinates,
// which is the one piece of arithmetic the page needs to know where the mouse
// is. Everything it compares against came from nanoclaude.
function inDrawing(event) {
  const ctm = groups.spots.getScreenCTM();
  if (!ctm) return { x: 0, y: 0 };
  const point = new DOMPoint(event.clientX, event.clientY).matrixTransform(ctm.inverse());
  return { x: point.x, y: point.y };
}

// turnDrawing shows the angle the user is on right now, without waiting for
// the next picture from nanoclaude.
//
// The drawing that arrived was computed at shownRotation. Turning the group by
// the difference makes dragging feel immediate, and the next picture lands on
// the same angle and cancels it out. SVG turns clockwise and the rotation is
// counter-clockwise, hence the sign.
function turnDrawing() {
  const delta = -shortest(state.rotation - shownRotation);
  const transform = `rotate(${delta.toFixed(2)})`;
  for (const group of Object.values(groups)) group.setAttribute("transform", transform);
}

// -- controls ---------------------------------------------------------------

// showRotation puts the angle on screen, in the readout, in the slider, and
// in the dial's accessible value.
function showRotation() {
  el("angle").textContent = `${state.rotation}°`;
  el("slider").value = String(state.rotation);
  dial.setAttribute("aria-valuenow", String(state.rotation));
  dial.setAttribute("aria-valuetext", `${state.rotation} degrees`);
  turnDrawing();
}

function setRotation(deg) {
  state.rotation = wrap(deg);
  showRotation();
  send({ rotation: state.rotation });
}

function step(by) {
  if (!el("snap").checked) return setRotation(state.rotation + by);
  const size = Math.abs(by) >= 15 ? 15 : 1;
  const base = Math.round(state.rotation / size) * size;
  setRotation(base + Math.sign(by) * size);
}

for (const button of document.querySelectorAll(".step")) {
  button.addEventListener("click", () => step(Number(button.dataset.turn)));
}

el("slider").addEventListener("input", (event) => setRotation(Number(event.target.value)));
el("numbers").addEventListener("change", (event) => {
  groups.labels.style.display = event.target.checked ? "" : "none";
});
groups.labels.style.display = "none";

// Dragging the shape. The angle is measured with Y pointing up, the way the
// wall does, so turning the mouse to the left turns the shape to the left.
function angleAt(event) {
  const box = svg.getBoundingClientRect();
  const cx = box.left + box.width / 2;
  const cy = box.top + box.height / 2;
  return (Math.atan2(cy - event.clientY, event.clientX - cx) * 180) / Math.PI;
}

dial.addEventListener("pointerdown", (event) => {
  if (!live) return;
  // A panel waiting to be dropped takes the click: the wall is where it
  // lands, so pressing here places it rather than turning the shape. That is
  // also the whole interaction on a touchscreen, where there is no dragging
  // from a button that keeps the pointer.
  if (editor.armed !== null) {
    markNearest(event);
    drop();
    return;
  }
  dial.setPointerCapture(event.pointerId);
  drag = { last: angleAt(event), from: state.rotation, moved: 0 };
});

dial.addEventListener("pointermove", (event) => {
  if (editor.armed !== null) {
    markNearest(event);
    return;
  }
  if (!drag) return;
  const now = angleAt(event);
  drag.moved += shortest(now - drag.last);
  drag.last = now;
  let next = drag.from + drag.moved;
  if (el("snap").checked) next = Math.round(next / 15) * 15;
  setRotation(next);
});

for (const ended of ["pointerup", "pointercancel"]) {
  dial.addEventListener(ended, () => {
    drag = null;
  });
}

addEventListener("keydown", (event) => {
  const tag = event.target instanceof Element ? event.target.tagName : "";
  if (tag === "INPUT" || tag === "SELECT" || tag === "BUTTON") return;
  switch (event.key) {
    case "ArrowLeft":
      step(15);
      break;
    case "ArrowRight":
      step(-15);
      break;
    case ",":
      step(1);
      break;
    case ".":
      step(-1);
      break;
    default:
      return;
  }
  event.preventDefault();
});

for (const radio of document.querySelectorAll("input[name=mode]")) {
  radio.addEventListener("change", () => {
    state.mode = radio.value;
    el("gauge-controls").hidden = state.mode !== "gauge";
    send({ mode: state.mode });
  });
}

el("level").addEventListener("input", (event) => {
  const percent = Number(event.target.value);
  el("level-out").textContent = `${percent}%`;
  state.level = percent / 100;
  send({ level: state.level });
});

el("phase").addEventListener("change", (event) => {
  state.phase = event.target.value;
  send({ phase: state.phase });
});

el("arrangement").addEventListener("change", async (event) => {
  state.shape = event.target.value;
  await send({ shape: state.shape });
  // The facts describe the arrangement, so they change with it.
  await loadFacts();
});

// -- building a wall -------------------------------------------------------

// placeable greys out the panels that have nowhere to go on this wall, which
// is how a page says that a Canvas square does not clip to a Shapes triangle
// without anybody having to read it.
function placeable(info) {
  const allowed = new Set(info.placeable || []);
  for (const button of document.querySelectorAll(".palette button")) {
    const canGo = allowed.has(Number(button.dataset.shape));
    button.disabled = !canGo;
    button.title = canGo ? "" : "Nowhere on this wall: it does not clip to what is already there.";
  }
}

function palette(info) {
  editor.kinds = info.kinds || [];
  editor.name = info.buildShape || "";
  const holder = el("palette");
  holder.replaceChildren();

  for (const kind of editor.kinds) {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = kind.label;
    button.dataset.shape = String(kind.shape);
    button.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      button.setPointerCapture(event.pointerId);
      arm(kind.shape, button);
    });
    button.addEventListener("pointermove", (event) => {
      if (editor.armed === kind.shape) markNearest(event);
    });
    button.addEventListener("pointerup", (event) => {
      if (editor.armed !== kind.shape) return;
      // A click without a drag leaves the panel armed, so it can be
      // dropped with a second click. That is also how this works on a
      // touchscreen.
      if (event.movementX === 0 && event.movementY === 0 && editor.nearest === null) return;
      markNearest(event);
      drop();
    });
    holder.append(button);
  }
}

// arm picks up a panel: nanoclaude is told what is being dragged, and answers
// with every place it could go.
async function arm(shape, button) {
  if (editor.armed === shape) {
    disarm();
    return;
  }
  editor.armed = shape;
  dial.classList.add("placing");
  editor.nearest = null;
  for (const other of document.querySelectorAll(".palette button")) {
    other.classList.toggle("armed", other === button);
  }
  await send({ placing: shape });
}

function disarm() {
  dial.classList.remove("placing");
  editor.armed = null;
  editor.nearest = null;
  editor.spots = [];
  groups.spots.replaceChildren();
  for (const button of document.querySelectorAll(".palette button")) {
    button.classList.remove("armed");
  }
  send({ placing: -1 });
}

// drop places the armed panel on the spot under the cursor.
async function drop() {
  const spot = editor.nearest === null ? null : editor.spots[editor.nearest];
  if (!spot) return;

  const shape = editor.armed;
  editor.armed = null;
  editor.nearest = null;
  dial.classList.remove("placing");
  for (const button of document.querySelectorAll(".palette button")) {
    button.classList.remove("armed");
  }

  const body =
    spot.panel < 0
      ? { action: "place", shape }
      : { action: "attach", shape, panel: spot.panel, edge: spot.edge, half: spot.half };
  const res = await post("edit", body);
  if (res) {
    setStatus("");
    // The wall changed, and the facts describe the wall.
    await loadFacts();
  }
  groups.spots.replaceChildren();
  editor.spots = [];
}

// Clicking a panel takes it off, when that tool is on.
groups.panels.addEventListener("click", async (event) => {
  if (!el("remove-tool").checked) return;
  const id = event.target instanceof Element ? event.target.dataset.panel : null;
  if (!id) return;
  if (await post("edit", { action: "remove", panel: Number(id) })) await loadFacts();
});

el("remove-tool").addEventListener("change", (event) => {
  if (event.target.checked) disarm();
  for (const node of groups.panels.children) {
    node.classList.toggle("removable", event.target.checked);
  }
});

for (const [id, action] of [["undo", "undo"], ["clear", "clear"]]) {
  el(id).addEventListener("click", async () => {
    if (await post("edit", { action })) await loadFacts();
  });
}

addEventListener("keydown", (event) => {
  if (event.key === "Escape" && editor.armed !== null) disarm();
});

el("save").addEventListener("click", async () => {
  const res = await post("save", { rotation: state.rotation });
  if (!res) return;
  const body = await res.json();
  setStatus(
    `Saved ${body.rotation}° to ${body.configPath}. Restart the display to pick it up: nanoclaude down && nanoclaude up`,
    "ok",
  );
});

el("done").addEventListener("click", async () => {
  if (stream) stream.close();
  await post("done", {});
  stop("Done. Your panels are back to what they were, and you can close this tab.", "ok");
});

// -- start ------------------------------------------------------------------

// loadFacts reads what does not change while the shape is being turned, and
// puts it on the page. Called again after the arrangement changes, because
// then it does.
async function loadFacts() {
  const res = await fetch(`${api}/info`);
  if (!res.ok) {
    stop(`nanoclaude refused this page: ${(await res.text()).trim()}`, "bad");
    return null;
  }
  const info = await res.json();
  facts(info);
  placeable(info);
  return info;
}

function shapePicker(info) {
  const select = el("arrangement");
  if (!info.shapes || info.shapes.length < 2) return;

  select.replaceChildren();
  for (const shape of info.shapes) {
    const option = document.createElement("option");
    option.value = shape.name;
    option.textContent = shape.label;
    select.append(option);
  }
  select.value = info.shape;
  el("arrangement-card").hidden = false;
}

function facts(info) {
  const rows = [
    ["Panels", `${info.lit} lit of ${info.panels}`],
    ["Side length", `${info.sideLength} units`],
    ["Device orientation", `${info.globalOrientation}°`],
    ["Configuration", info.configPath || "not written yet"],
  ];
  const list = el("facts");
  list.replaceChildren();
  for (const [name, value] of rows) {
    const dt = document.createElement("dt");
    dt.textContent = name;
    const dd = document.createElement("dd");
    dd.textContent = value;
    list.append(dt, dd);
  }

  if (info.unknownShapes && info.unknownShapes.length) {
    const note = el("unknown");
    note.textContent =
      `This version does not know shape ${info.unknownShapes.join(", ")}, so those panels are drawn ` +
      `as circles. If they have no LEDs, set NANOCLAUDE_SKIP_SHAPES to skip them.`;
    note.hidden = false;
  }
  el("fighting").hidden = !info.displayRunning;

  // Three things this page can be, and they do not read the same. A wall
  // being built is nobody's yet, a sample is somebody else's, and the panels
  // on your own wall are the only ones a picture is sent to.
  const building = Boolean(info.buildShape) && info.shape === info.buildShape;
  if (building) {
    document.querySelector("h1").textContent = "Build a wall and see the display on it";
    el("intro").textContent =
      "Drag panels from the palette onto the wall. Nothing is sent to a device, and the " +
      "display is drawn on whatever you build: green at the bottom of the shape, red at the top.";
  } else if (!info.live) {
    document.querySelector("h1").textContent = "See the display on other shapes";
    el("intro").textContent =
      "This is a sample arrangement, so the page is the whole display and nothing is being " +
      "sent to a device. The bottom of the shape is green and the top is red.";
  }
  el("save").hidden = !info.canSave;

  el("build-card").hidden = !building;
  if (!building) disarmQuietly();
  if (building) {
    el("build-hint").textContent =
      info.panels === 0
        ? "Drag a panel onto the middle of the wall to start."
        : "Drag a panel onto the wall. It sticks to whichever edge you drop it on, and an edge " +
          "that is taken will not take another.";
  }
}

// disarmQuietly forgets a dragged panel without telling the server, for when
// the page has moved to another arrangement and there is nothing to tell.
function disarmQuietly() {
  dial.classList.remove("placing");
  editor.armed = null;
  editor.nearest = null;
  editor.spots = [];
  groups.spots.replaceChildren();
  for (const button of document.querySelectorAll(".palette button")) {
    button.classList.remove("armed");
  }
}

async function start() {
  try {
    const info = await loadFacts();
    if (!info) return;
    shapePicker(info);
    palette(info);

    state.shape = info.shape;
    state.mode = info.mode;
    state.level = info.level;
    state.phase = info.phase;
    const percent = Math.round(info.level * 100);
    el("level").value = String(percent);
    el("level-out").textContent = `${percent}%`;
    el("phase").value = info.phase;
    for (const radio of document.querySelectorAll("input[name=mode]")) {
      radio.checked = radio.value === info.mode;
    }
    el("gauge-controls").hidden = info.mode !== "gauge";

    state.rotation = wrap(info.rotation);
    shownRotation = state.rotation;
    showRotation();
  } catch {
    stop("Could not reach nanoclaude.", "bad");
    return;
  }

  stream = new EventSource(`${api}/events`);
  stream.addEventListener("message", (event) => draw(JSON.parse(event.data)));
  stream.addEventListener("error", () => {
    // The browser retries on its own, so a single error is not the end.
    // A closed stream is.
    if (stream.readyState === EventSource.CLOSED) {
      stop("nanoclaude has stopped. Your panels are back to what they were.", "bad");
      return;
    }
    setStatus("Lost the live picture. Reconnecting.", "bad");
  });
  stream.addEventListener("open", () => {
    if (live && el("status").classList.contains("bad")) setStatus("");
  });
}

start();
