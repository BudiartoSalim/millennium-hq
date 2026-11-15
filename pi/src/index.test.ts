import assert from "node:assert/strict";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { exited, fakeContext, fakeRun } from "./__tests__/fakes.ts";
import millennium, { identity, setupFor, wire, type Setup } from "./index.ts";
import type { PiAPI } from "./pi-api.ts";

// fakePi records what the extension registers.
function fakePi() {
  const handlers = new Map<string, unknown>();
  const tools: string[] = [];
  const pi = {
    on: (event: string, handler: unknown) => handlers.set(event, handler),
    registerTool: (tool: { name: string }) => tools.push(tool.name),
  } as unknown as PiAPI;
  return { pi, handlers, tools };
}

function setup(env: Record<string, string | undefined>): Setup {
  const real = setupFor("/pkg/pi/src/index.ts", { env, platform: "linux", execPath: "/usr/bin/node", argv: ["/usr/bin/node", "/nonexistent/cli.js"] }, "/home/u");
  return { ...real, spawner: { ...real.spawner, run: fakeRun(() => exited("")).run } };
}

test("wire registers the handlers and, in the main session, the agent tool", async () => {
  const { pi, handlers, tools } = fakePi();
  wire(pi, setup({}));
  assert.deepEqual([...handlers.keys()], ["session_start", "before_agent_start", "tool_call", "agent_before_settle"]);
  assert.deepEqual(tools, ["millennium_agent"]);
  const guard = handlers.get("tool_call") as (e: unknown, c: unknown) => Promise<unknown>;
  assert.equal(await guard({ toolName: "read", toolCallId: "c", input: { path: "a" } }, fakeContext()), undefined);
});

test("an agent gets no agent tool of its own", () => {
  const { pi, tools } = fakePi();
  wire(pi, setup({ MILLENNIUM_AGENT: "qa-tester" }));
  assert.deepEqual(tools, []);
});

test("identity reads the agent from the environment", () => {
  assert.deepEqual(identity(fakeContext(), {}), { sessionId: "session-1", agentId: "", agentType: "" });
  assert.deepEqual(identity(fakeContext(), { MILLENNIUM_AGENT: "qa-tester", MILLENNIUM_AGENT_ID: "qa-tester-1" }), {
    sessionId: "session-1",
    agentId: "qa-tester-1",
    agentType: "qa-tester",
  });
});

test("setupFor finds the plugin beside the extension and keeps state in pi's agent directory", () => {
  const cli = fileURLToPath(import.meta.url); // a file that exists, standing in for pi's cli.js
  const s = setupFor("/pkg/pi/src/index.ts", { env: { PATH: "/bin" }, platform: "linux", execPath: "/usr/bin/node", argv: ["/usr/bin/node", cli] }, "/home/u");
  assert.deepEqual(s.launch, {
    shell: "/bin/sh",
    launcher: "/pkg/plugin/scripts/millennium-code-qc",
    env: { PATH: "/bin", MILLENNIUM_PLUGIN_ROOT: "/pkg/plugin", MILLENNIUM_STATE_DIR: "/home/u/.pi/agent/millennium/state" },
  });
  assert.deepEqual(s.spawner.pi, { command: "/usr/bin/node", args: [cli] });
  assert.equal(s.spawner.extension, "/pkg/pi/src/index.ts");
  assert.equal(s.home, "/home/u");
});

test("setupFor honours pi's and millennium's directory settings, and Git Bash on Windows", () => {
  const custom = setupFor("/pkg/pi/src/index.ts", { env: { PI_CODING_AGENT_DIR: "~/pi" }, platform: "linux", execPath: "/bin/pi", argv: ["/bin/pi"] }, "/home/u");
  assert.equal(custom.spawner.stateDir, "/home/u/pi/millennium/state");
  assert.deepEqual(custom.spawner.pi, { command: "/bin/pi", args: [] });
  const state = setupFor("/pkg/pi/src/index.ts", { env: { MILLENNIUM_STATE_DIR: "/s" }, platform: "linux", execPath: "/bin/pi", argv: [] }, "/home/u");
  assert.equal(state.spawner.stateDir, "/s");
  const windows = setupFor("/pkg/pi/src/index.ts", { env: { ProgramFiles: "/pf" }, platform: "win32", execPath: "/bin/pi", argv: [] }, "/home/u");
  assert.equal(windows.launch.shell, "/pf/Git/bin/bash.exe");
  const defaults = setupFor("/pkg/pi/src/index.ts", { env: {}, platform: "win32", execPath: "/bin/pi", argv: [] }, "/home/u");
  assert.match(defaults.launch.shell, /Program Files.Git.bin.bash\.exe$/);
});

test("the entry point wires the extension on every load, as after /reload", () => {
  for (let load = 0; load < 2; load++) {
    const { pi, handlers } = fakePi();
    millennium(pi);
    assert.equal(handlers.size, 4);
  }
});
