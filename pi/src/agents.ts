// Reads millennium's agent definitions (plugin/agents/*.md, written for
// Claude Code) and turns each into what a pi process needs: its tools and
// its system prompt, with the skills the agent preloads.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";

/** Agent is one agent definition, in pi's terms. */
export interface Agent {
  name: string;
  description: string;
  /** pi tool names. */
  tools: string[];
  /** Skill names without the plugin prefix. */
  skills: string[];
  /** Whether the agent works in its own git worktree. */
  worktree: boolean;
  body: string;
}

// The agents the millennium_agent tool starts. The tech lead runs in the
// main session in pi (/skill:tech-lead), because pi agents cannot start
// agents of their own.
export const agentNames = [
  "software-engineer",
  "correctness-reviewer",
  "maintainability-reviewer",
  "performance-reviewer",
  "security-engineer",
  "qa-tester",
] as const;

export type AgentName = (typeof agentNames)[number];

export function isAgentName(name: string): name is AgentName {
  return (agentNames as readonly string[]).includes(name);
}

// pi's built-in tools for each Claude Code tool. Skill needs none (pi agents
// read skill files), and pi has no web tools: agents fetch with curl.
const toolMap: Record<string, string[]> = {
  Read: ["read"],
  Grep: ["grep"],
  Glob: ["find", "ls"],
  Bash: ["bash"],
  Edit: ["edit"],
  Write: ["write"],
  Skill: [],
  WebFetch: [],
  WebSearch: [],
  Agent: [],
  NotebookEdit: [],
};
const allTools = ["read", "bash", "edit", "write", "grep", "find", "ls"];

/** parseAgent reads an agent definition; anything unexpected is an error. */
export function parseAgent(text: string): Agent {
  const match = /^---\n([\s\S]*?)\n---\n([\s\S]*)$/.exec(text.replaceAll("\r\n", "\n"));
  if (match === null) throw new Error("the agent definition has no frontmatter");
  const fields = parseFrontmatter(match[1] ?? "");
  const name = fields.get("name");
  if (typeof name !== "string" || name === "") throw new Error("the agent definition has no name");
  const tools = typeof fields.get("tools") === "string" ? mapTools(fields.get("tools")) : allTools;
  const denied = mapTools(fields.get("disallowedTools") ?? "");
  const skills = fields.get("skills") ?? [];
  if (!Array.isArray(skills)) throw new Error(`agent ${name}: skills must be a list`);
  return {
    name,
    description: str(fields.get("description")),
    tools: tools.filter((tool) => !denied.includes(tool)),
    skills: skills.map((skill) => skill.slice(skill.lastIndexOf(":") + 1)),
    worktree: fields.get("isolation") === "worktree",
    body: (match[2] ?? "").trim(),
  };
}

// parseFrontmatter reads the YAML subset the agent files use: `key: value`
// lines and `key:` followed by `  - item` lines.
function parseFrontmatter(text: string): Map<string, string | string[]> {
  const fields = new Map<string, string | string[]>();
  let list: string[] | undefined;
  for (const line of text.split("\n")) {
    const item = /^\s+-\s+(.+)$/.exec(line);
    const pair = /^([A-Za-z]+):\s*(.*)$/.exec(line);
    if (item !== null && list !== undefined) {
      list.push((item[1] ?? "").trim());
    } else if (pair !== null) {
      const key = pair[1] ?? "";
      const value = (pair[2] ?? "").trim();
      list = value === "" ? [] : undefined;
      fields.set(key, list ?? value);
    } else if (line.trim() !== "") {
      throw new Error(`unexpected frontmatter line: ${line}`);
    }
  }
  return fields;
}

function mapTools(value: string | string[] | undefined): string[] {
  const tools: string[] = [];
  for (const name of str(value).split(",").map((t) => t.trim()).filter((t) => t !== "")) {
    const mapped = toolMap[name];
    if (mapped === undefined) throw new Error(`unknown tool ${name}`);
    for (const tool of mapped) if (!tools.includes(tool)) tools.push(tool);
  }
  return tools;
}

function str(value: string | string[] | undefined): string {
  return typeof value === "string" ? value : "";
}

/** loadAgent reads an agent of the plugin at pluginRoot. */
export function loadAgent(pluginRoot: string, name: AgentName): Agent {
  return parseAgent(readFileSync(join(pluginRoot, "agents", `${name}.md`), "utf8"));
}

/**
 * systemPrompt is the agent's instructions followed by its preloaded skills,
 * each in the form pi uses when a skill is invoked.
 */
export function systemPrompt(pluginRoot: string, agent: Agent): string {
  const parts = [agent.body];
  for (const skill of agent.skills) {
    const file = join(pluginRoot, "skills", skill, "SKILL.md");
    const body = readFileSync(file, "utf8").replaceAll("\r\n", "\n").replace(/^---\n[\s\S]*?\n---\n/, "").trim();
    parts.push(`<skill name="${skill}" location="${file}">\nReferences are relative to ${dirname(file)}.\n\n${body}\n</skill>`);
  }
  return parts.join("\n\n");
}
