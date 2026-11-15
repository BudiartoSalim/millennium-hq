import assert from "node:assert/strict";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { agentNames, isAgentName, loadAgent, parseAgent, systemPrompt } from "./agents.ts";

const pluginRoot = fileURLToPath(new URL("../../plugin", import.meta.url).href);

test("every agent the tool offers loads from the plugin, with pi tools", () => {
  const want: Record<string, string[]> = {
    "software-engineer": ["read", "edit", "write", "bash", "grep", "find", "ls"],
    "correctness-reviewer": ["read", "grep", "find", "ls", "bash", "edit", "write"],
    "maintainability-reviewer": ["read", "grep", "find", "ls", "bash"],
    "performance-reviewer": ["read", "grep", "find", "ls", "bash"],
    "security-engineer": ["read", "grep", "find", "ls", "bash"],
    "qa-tester": ["read", "bash", "edit", "write", "grep", "find", "ls"],
  };
  for (const name of agentNames) {
    const agent = loadAgent(pluginRoot, name);
    assert.equal(agent.name, name);
    assert.deepEqual(agent.tools, want[name], name);
    assert.ok(agent.skills.length > 0 && agent.body.length > 0, name);
  }
  const engineer = loadAgent(pluginRoot, "software-engineer");
  assert.deepEqual(engineer.skills, ["engineering-standards"]);
  assert.equal(engineer.worktree, true);
  assert.equal(loadAgent(pluginRoot, "maintainability-reviewer").worktree, false);
});

test("systemPrompt adds each preloaded skill in pi's skill form", () => {
  const prompt = systemPrompt(pluginRoot, loadAgent(pluginRoot, "security-engineer"));
  for (const skill of ["review-protocol", "security-review", "threat-model", "security-research"]) {
    assert.match(prompt, new RegExp(`<skill name="${skill}" location="[^"]+/skills/${skill}/SKILL\\.md">\\nReferences are relative to `));
  }
  assert.doesNotMatch(prompt, /\nname: security-review\n/); // frontmatter stripped
});

test("parseAgent reads lists, denied tools and CRLF files", () => {
  const agent = parseAgent("---\r\nname: x\r\ndescription: a: b\r\ndisallowedTools: Agent, Write\r\nskills:\r\n  - millennium:one\r\n  - two\r\n---\r\nbody\r\n");
  assert.deepEqual(agent, {
    name: "x",
    description: "a: b",
    tools: ["read", "bash", "edit", "grep", "find", "ls"],
    skills: ["one", "two"],
    worktree: false,
    body: "body",
  });
});

test("parseAgent refuses what it does not understand", () => {
  const cases: Array<[string, RegExp]> = [
    ["no frontmatter", /no frontmatter/],
    ["---\ndescription: d\n---\n", /no name/],
    ["---\nname: x\ntools: Read, Teleport\n---\n", /unknown tool Teleport/],
    ["---\nname: x\nskills: one\n---\n", /skills must be a list/],
    ["---\nname: x\n  - stray\n---\n", /unexpected frontmatter line/],
  ];
  for (const [text, want] of cases) assert.throws(() => parseAgent(text), want);
});

test("isAgentName accepts only the agents the tool starts", () => {
  assert.equal(isAgentName("qa-tester"), true);
  assert.equal(isAgentName("tech-lead"), false);
});
