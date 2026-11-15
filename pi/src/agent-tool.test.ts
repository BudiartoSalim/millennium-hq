import assert from "node:assert/strict";
import { after, test } from "node:test";
import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { exited, fakeContext, fakeRun, type Call } from "./__tests__/fakes.ts";
import { AgentTool, finalAnswer, toolName, type Spawner } from "./agent-tool.ts";
import type { ProcessResult } from "./process.ts";

const pluginRoot = fileURLToPath(new URL("../../plugin", import.meta.url).href);
const stateDir = mkdtempSync(join(tmpdir(), "millennium-agents-"));
after(() => rmSync(stateDir, { recursive: true, force: true }));

const answer = (text: string) => JSON.stringify({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text }] } });

// agentTool answers git with a repository at /repo and pi with `pi`.
function agentTool(pi: (call: Call) => ProcessResult) {
  const fake = fakeRun((call) => {
    if (call.command === "git") return exited(call.args.includes("rev-parse") ? "/repo\n" : "");
    return pi(call);
  });
  const spawner: Spawner = {
    run: fake.run,
    pi: { command: "/usr/bin/node", args: ["/pi/cli.js"] },
    extension: "/pkg/pi/src/index.ts",
    pluginRoot,
    stateDir,
    env: { MILLENNIUM_PLUGIN_ROOT: pluginRoot },
  };
  return { tool: new AgentTool(spawner), calls: fake.calls };
}

function streamed(call: Call, text: string): ProcessResult {
  for (const line of ["{not json", answer("draft"), answer(text)]) call.options.onLine?.(line);
  return exited("");
}

test("a new agent runs as a pi process in its own worktree, with its role", async () => {
  const { tool, calls } = agentTool((call) => streamed(call, "P1 done"));
  const result = await tool.execute({ tasks: [{ agent: "software-engineer", task: "-build P1" }] }, undefined, fakeContext());
  const text = result.content[0]?.text ?? "";
  const id = /id (software-engineer-[0-9a-f]{8})/.exec(text)?.[1] ?? "";
  assert.match(text, new RegExp(`^## Task 1\\nagent software-engineer, id ${id}, in worktree /repo/.pi/worktrees/${id} \\(branch millennium/${id}\\)\\n\\nP1 done$`));
  assert.deepEqual(calls.filter((c) => c.command === "git").map((c) => c.args.slice(2, 4)), [["rev-parse", "--show-toplevel"], ["worktree", "add"]]);
  const pi = calls.find((c) => c.command === "/usr/bin/node");
  assert.deepEqual(pi?.args, [
    "/pi/cli.js", "--mode", "json", "--print", "--session", join(stateDir, "agents", `${id}.jsonl`),
    "--tools", "read,edit,write,bash,grep,find,ls", "--append-system-prompt", join(stateDir, "agents", `${id}.prompt.md`),
    "--extension", "/pkg/pi/src/index.ts", "--model", "anthropic/claude-opus-5-5", "--", "Task:\n-build P1",
  ]);
  assert.equal(pi?.options.cwd, `/repo/.pi/worktrees/${id}`);
  assert.equal(pi?.options.env["MILLENNIUM_AGENT"], "software-engineer");
  assert.equal(pi?.options.env["MILLENNIUM_AGENT_ID"], id);
  assert.match(readFileSync(join(stateDir, "agents", `${id}.prompt.md`), "utf8"), /<skill name="engineering-standards"/);
});

test("a reviewer without a worktree works in the session's directory, and resumes there", async () => {
  const { tool, calls } = agentTool((call) => streamed(call, "no findings"));
  const ctx = { ...fakeContext("/repo/sub"), model: undefined };
  const first = await tool.execute({ tasks: [{ agent: "maintainability-reviewer", task: "review" }] }, undefined, ctx);
  const id = /id (maintainability-reviewer-[0-9a-f]{8}), in \/repo\/sub\n/.exec(first.content[0]?.text ?? "")?.[1] ?? "";
  assert.notEqual(id, "");
  assert.equal(calls.some((c) => c.command === "git"), false);
  assert.equal(calls[0]?.args.includes("--model"), false);
  await tool.execute({ tasks: [{ resume: id, task: "and the tests?" }] }, undefined, ctx);
  assert.equal(calls[1]?.options.cwd, "/repo/sub");
  assert.equal(calls[1]?.args[5], join(stateDir, "agents", `${id}.jsonl`));
});

test("tasks run in parallel and fail one by one", async () => {
  const { tool } = agentTool((call) =>
    call.options.env["MILLENNIUM_AGENT"] === "qa-tester" ? { ...exited("", 1, "model error"), code: 1 } : streamed(call, "ok"),
  );
  const result = await tool.execute(
    {
      tasks: [
        { agent: "performance-reviewer", task: "a" },
        { agent: "qa-tester", task: "b" },
        { agent: "tech-lead", task: "c" },
        { resume: "nobody", task: "d" },
        { agent: "qa-tester", resume: "nobody", task: "e" },
      ],
    },
    undefined,
    fakeContext(),
  );
  const text = result.content[0]?.text ?? "";
  assert.match(text, /## Task 1\nagent performance-reviewer, id performance-reviewer-\w+, in \/repo\n\nok/);
  assert.match(text, /## Task 2\nagent qa-tester, id qa-tester-\w+, in worktree .*\nFAILED: the agent exited with status 1\. model error\nLast answer:\n\(none\)/);
  assert.match(text, /## Task 3\nfailed: unknown agent "tech-lead"/);
  assert.match(text, /## Task 4\nfailed: no agent with id "nobody" was started in this session/);
  assert.match(text, /## Task 5\nfailed: no agent with id "nobody"/);
});

test("an agent works on one task at a time", async () => {
  const { tool } = agentTool((call) => streamed(call, "ok"));
  const first = await tool.execute({ tasks: [{ agent: "performance-reviewer", task: "a" }] }, undefined, fakeContext());
  const id = /id (performance-reviewer-\w+)/.exec(first.content[0]?.text ?? "")?.[1] ?? "";
  const both = await tool.execute({ tasks: [{ resume: id, task: "b" }, { resume: id, task: "c" }] }, undefined, fakeContext());
  const text = both.content[0]?.text ?? "";
  assert.match(text, /## Task 1\nagent performance-reviewer/);
  assert.match(text, new RegExp(`## Task 2\nfailed: agent ${id} is still working on another task`));
  const again = await tool.execute({ tasks: [{ resume: id, task: "d" }] }, undefined, fakeContext());
  assert.match(again.content[0]?.text ?? "", /\n\nok$/);
});

test("resuming an agent as another kind is refused", async () => {
  const { tool } = agentTool((call) => streamed(call, "ok"));
  const first = await tool.execute({ tasks: [{ agent: "security-engineer", task: "threat model" }] }, undefined, fakeContext());
  const id = /id (security-engineer-\w+)/.exec(first.content[0]?.text ?? "")?.[1] ?? "";
  const second = await tool.execute({ tasks: [{ agent: "qa-tester", resume: id, task: "x" }] }, undefined, fakeContext());
  assert.match(second.content[0]?.text ?? "", new RegExp(`failed: agent ${id} is a security-engineer, not a qa-tester`));
});

test("a hung or silent agent is reported as such", async () => {
  const { tool } = agentTool(() => ({ code: null, stdout: "", stderr: "", failure: "cancelled" }));
  const hung = await tool.execute({ tasks: [{ agent: "performance-reviewer", task: "a" }] }, undefined, fakeContext());
  assert.match(hung.content[0]?.text ?? "", /FAILED: the agent cancelled\./);
  const silent = agentTool(() => exited(""));
  const quiet = await silent.tool.execute({ tasks: [{ agent: "performance-reviewer", task: "a" }] }, undefined, fakeContext());
  assert.match(quiet.content[0]?.text ?? "", /\(the agent gave no answer\)$/);
});

test("a worktree that cannot be made fails the task", async () => {
  const fake = fakeRun(() => exited("", 128, "fatal: not a git repository"));
  const tool = new AgentTool({ run: fake.run, pi: { command: "pi", args: [] }, extension: "/e.ts", pluginRoot, stateDir, env: {} });
  const result = await tool.execute({ tasks: [{ agent: "qa-tester", task: "smoke" }] }, undefined, fakeContext());
  assert.match(result.content[0]?.text ?? "", /failed: git rev-parse failed: fatal: not a git repository/);
});

test("the tool definition offers the agents and runs execute", async () => {
  const { tool } = agentTool((call) => streamed(call, "fine"));
  const definition = tool.definition();
  assert.equal(definition.name, toolName);
  assert.match(JSON.stringify(definition.parameters), /"enum":\["software-engineer","correctness-reviewer"/);
  const result = await definition.execute("call-1", { tasks: [{ agent: "performance-reviewer", task: "a" }] }, undefined, undefined, fakeContext());
  assert.match(result.content[0]?.text ?? "", /fine$/);
  assert.equal(existsSync(join(stateDir, "agents")), true);
});

test("finalAnswer reads assistant text from pi's JSON events only", () => {
  assert.equal(finalAnswer(answer(" hi ")), "hi");
  assert.equal(finalAnswer("garbage"), undefined);
  assert.equal(finalAnswer(JSON.stringify({ type: "message_end", message: { role: "user", content: [{ type: "text", text: "x" }] } })), undefined);
  assert.equal(finalAnswer(JSON.stringify({ type: "message_end", message: { role: "assistant", content: "x" } })), undefined);
  assert.equal(finalAnswer(JSON.stringify({ type: "message_end", message: { role: "assistant", content: [{ type: "toolCall" }] } })), undefined);
  assert.equal(finalAnswer(JSON.stringify({ type: "agent_end" })), undefined);
});
