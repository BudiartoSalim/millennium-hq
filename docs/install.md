# Installing millennium

millennium works at three levels, depending on what your harness lets it
hook into:

| Harness | What you get | How |
|---|---|---|
| **Claude Code** | Everything: the guard on every tool call, the gates when an agent finishes, the OS sandbox rules, the agents and skills. | [The plugin](#claude-code) |
| **pi** | The guard, the gates and the agents, without an OS sandbox behind them. | [The pi package](#pi) |
| **Any other harness** that reads Agent Skills (Codex, OpenCode, Gemini CLI, ...) | The skills and the engine as a command. **Nothing is enforced**: the agent follows the skills because it is asked to. | [Skills only](#other-harnesses-skills-only) |

Whatever you use, the rules live in the repository (`.millennium/config.json`)
and the engine checks them the same way. Repositories without a config are
left alone.

## What every setup needs

- **git**, and **`curl` or `wget`** plus **`sha256sum` or `shasum`**: the
  launcher uses these to download the engine and verify it.
- **The toolchains of the languages you use**, because the gates run them:
  Go; `node` and the project's installed dependencies for TypeScript
  (TypeScript 6 or older); Python 3.10 or newer; `bash` and `shellcheck` for
  shell scripts (or `"shell": {"lint": "off"}` in the config). A gate whose
  tool is missing blocks rather than skipping, and says what to install.
- **Network access to github.com** the first time, to download the engine
  release (GitHub redirects the download to its own asset host). `curl` and
  `wget` honour `HTTPS_PROXY`. To work offline, see
  [Your own engine binary](#your-own-engine-binary).
- **On Windows**, Git for Windows: hooks and the launcher run with Git Bash.
  Linux and macOS need nothing extra. The Windows path is less tested than the
  others.

You do not need Go or a build step. The engine is a single binary the
launcher downloads for your platform (Linux, macOS and Windows, amd64 and
arm64) on first use. It refuses the binary unless its SHA-256 matches the
lock that ships with the plugin, and checks it again on every run. See
[Hooks: Finding the engine](hooks.md#finding-the-engine).

## Claude Code

### Install

In Claude Code:

```
/plugin marketplace add BudiartoSalim/millennium-hq
/plugin install millennium@millennium-hq
```

or from your terminal:

```
claude plugin marketplace add BudiartoSalim/millennium-hq
claude plugin install millennium@millennium-hq
```

Start a new session. The first session downloads the engine; after that,
startup costs nothing noticeable.

### User or project install

A user install (the default) enables the plugin in every session on your
machine. Its hooks run in every repository, but outside an opted-in
repository the engine only checks for a config and exits, so they never get
in the way.

There is no special way to start a session with the plugin: an enabled
plugin is part of every session in its scope, and you run `claude` as usual.
Its skills do nothing until you invoke them, like any plugin's commands. Only
its hooks run on their own, and only in opted-in repositories.

**On only where you use it.** If you would rather the plugin be completely
absent from other repositories:

1. Install it, then disable it in `/plugin`, which turns it off for your
   user.
2. Opt a repository in. `config apply` writes
   `"enabledPlugins": {"millennium@millennium-hq": true}` into the project's
   `.claude/settings.json`, and project settings take precedence over user
   settings.

The plugin is then on in opted-in repositories and off everywhere else.
Check it once: `/millennium:` should autocomplete in an opted-in repository
and not elsewhere. Alternatively, install with `--scope project` in each
repository.

Each person who works in the repository installs the plugin on their own
machine; the project's settings only enable it.

### Opt a repository in

Run `/millennium:adopt-project` for existing code, or
`/millennium:init-project` for a new project. The agent drafts
`millennium.proposal.json` with you and shows what the rules would find
today. You apply it yourself, with the command the agent gives you:

```
! "<plugin>/scripts/millennium-code-qc" config apply
```

It lists every file it writes and every way it loosens the rules, and asks
you to confirm. Agents cannot run it. Besides `.millennium/config.json`, it
writes the project's `.claude/settings.json`: sandbox rules that protect
`.millennium/` from shell commands, and the plugin enabled for the project.
Commit both. Start a new session to pick up the gates.

Then lead changes with `/millennium:tech-lead <task>`; see
[Agents and skills](agents.md).

### Keep the sandbox on

The guard matches shell commands only best-effort. Claude Code's OS sandbox
is what stops a command from writing a protected path, so keep it enabled and
do not approve unsandboxed retries. See
[Hooks: Recommended settings](hooks.md#recommended-settings).

### Update and remove

Update the marketplace (`/plugin marketplace update millennium-hq`), then
update the plugin from `/plugin`. A plugin update that names a new engine
release downloads it in the next session.

`/plugin uninstall millennium@millennium-hq` removes the plugin.
Repositories keep their `.millennium/` config, so the terminal commands still
work, and reinstalling picks up where you left off.

### Alongside your own instructions

The plugin adds to what Claude Code loads; it replaces nothing. Your
`CLAUDE.md` files (user, project, parent directories, `CLAUDE.local.md`), the
project's `.claude/skills/` and `.claude/agents/`, and your own hooks all
work as before.

- **AGENTS.md.** Claude Code reads `CLAUDE.md`; it falls back to `AGENTS.md`
  only when no `CLAUDE.md` applies, and a `CLAUDE.md` in a parent directory
  is enough to stop that. `config apply` writes a "Quality gates
  (millennium)" section into `AGENTS.md`, between markers it owns, and adds
  an `@AGENTS.md` import to the project's `CLAUDE.md` if there is one. If you
  create a `CLAUDE.md` later, give it that import yourself.
- **Skill names.** The plugin's skills are namespaced (`/millennium:tech-lead`),
  so a project skill with the same name (`/tech-lead`) does not clash. Do not
  also copy millennium's skills into `.claude/skills/` in a repository that
  uses the plugin: you would have two copies, and the copy cannot fill in
  `${CLAUDE_PLUGIN_ROOT}`.

**When your instructions conflict with millennium's**, what happens depends
on the kind of rule:

| Kind of rule | Example in your CLAUDE.md | What happens |
|---|---|---|
| Enforced by the engine | "tests are optional", "raise the limit in `.millennium/config.json` if it gets in the way", "suppress lint freely" | millennium wins, always. The gates block failing work, writes to `.millennium/` are denied, and a suppression without a reason is a finding. The agent is sent back until the gates pass. |
| Workflow and judgement | "commit straight to main", "don't use subagents", "skip review for small changes" | The model decides. A skill you just invoked usually wins because it is specific and recent, but contradictory instructions can go either way, or the agent asks you. Nothing checks this. |

So keep your instructions consistent with the config: remove rules the gates
contradict, and change the config (through a proposal) when the gates are
what is wrong.

### Try a change to the plugin

To run the plugin from a clone of this repository (for example, to test a
change), start Claude Code with `--plugin-dir`:

```
claude --plugin-dir /path/to/millennium-hq/plugin
```

If the change touches the engine, build it (`cd code-qc && go build -o
millennium-code-qc .`, Go 1.25 or newer) and set `MILLENNIUM_CODE_QC` to the
binary's absolute path. The lock only matches released engines.

## pi

```
pi install git:github.com/BudiartoSalim/millennium-hq
```

This installs for your user. Add `-l` to install it for the current project
only, in `.pi/settings.json`. Append `@<tag or commit>` to the source to pin
a version you have reviewed. Start a new pi session afterwards.

Skills become `/skill:<name>` (`/skill:adopt-project`, `/skill:tech-lead
<task>`). Agents run through the `millennium_agent` tool, as separate pi
processes in their own worktrees. Apply proposals with pi's `!` prefix, the
same way as in Claude Code.

What you should know before relying on it:

- **There is no sandbox.** The edit and write tools are blocked reliably,
  but a shell command can write `.millennium/` in ways the guard does not
  recognise. For hard guarantees, run pi in a container with those paths
  read-only. Either way, the tech lead's final full gate decides whether work
  is done.
- **Other extensions run with full access.** An extension you install can
  change a tool call after millennium has checked it. Install only
  extensions you trust.
- **Agents wait.** `millennium_agent` runs up to 4 agents in parallel, but
  your session waits until they finish.

[pi](pi.md) covers the details: how events map to the engine, the agent tool,
and everything pi cannot enforce.

## Other harnesses: skills only

Any harness that reads Agent Skills (folders containing a `SKILL.md`) can use
millennium's skills: the tech-lead workflow, project onboarding, dependency
vetting and the review playbooks. The engine works as a command your agent
and you can run. What is missing is enforcement.

### What you give up

| Without hooks | Consequence | What to do instead |
|---|---|---|
| No guard | Agents can edit `.millennium/`, weaken a tool config, or read code as the qa-tester. | Review every change to `.millennium/` and tool configs yourself; make them CODEOWNERS-protected if you use pull requests. |
| No gates when an agent finishes | An agent can stop with failing work. | Run `millennium-code-qc gate` yourself before you accept a change, or in a git pre-commit hook or CI. |
| No session briefing | Agents do not know the repository is checked unless something tells them. | Add the note below to the repository's `AGENTS.md` (or your harness's equivalent). |
| No millennium agents | The tech lead cannot start fresh-context engineers and reviewers by itself. | Use your harness's subagents, or run each role in a new session (below). |

The skills tell the agent to run the gates and never work around them. A
capable model mostly does, but nothing checks it. Treat a skills-only
setup as a strong workflow, not a guarantee.

### 1. Get the skills and the engine

Clone this repository somewhere outside your projects, and pin a version you
have reviewed:

```
git clone https://github.com/BudiartoSalim/millennium-hq ~/tools/millennium-hq
cd ~/tools/millennium-hq && git checkout code-qc/v0.2.0
```

The engine command is the launcher in the clone:
`~/tools/millennium-hq/plugin/scripts/millennium-code-qc`. On first use it
downloads the engine into `plugin/.code-qc/` in the clone and verifies it, as
in Claude Code. Put it on your `PATH` if you like:

```
ln -s ~/tools/millennium-hq/plugin/scripts/millennium-code-qc ~/.local/bin/millennium-code-qc
```

### 2. Install the skills

Copy (or symlink) every folder under `plugin/skills/` into your harness's
skills directory. Many harnesses read `.agents/skills/` in the project:

```
mkdir -p .agents/skills
cp -R ~/tools/millennium-hq/plugin/skills/* .agents/skills/
```

| Harness | Project skills | Your own skills (all projects) |
|---|---|---|
| Codex | `.agents/skills/` | `~/.agents/skills/` |
| OpenCode | `.opencode/skills/`, also `.agents/skills/` and `.claude/skills/` | `~/.config/opencode/skills/` |
| Gemini CLI | `.gemini/skills/` or `.agents/skills/` (skills must be enabled in its settings) | `~/.gemini/skills/` |

These paths were current in November 2026. Check your harness's
documentation, since skill support is still changing.

Install all fourteen skills, even if you invoke only a few: the tech lead and
the agents rely on the others by name (`review-protocol`,
`engineering-standards`, ...). If you already have a skill with the same name
(`security-review` is a common one), keep millennium's in a separate
location rather than renaming it.

A copy pins the skills; to update, pull the clone, check out a newer version,
and copy again. A symlink follows the clone. On Windows, copy.

### 3. Tell the agent where things are

The skills are written for Claude Code and use its names. Add this to the
repository's `AGENTS.md`, with your clone's path:

```markdown
## millennium

This repository is checked by millennium (`.millennium/config.json`). Its
skills are written for Claude Code; read them this way:

- `"${CLAUDE_PLUGIN_ROOT}/scripts/millennium-code-qc"` and
  `millennium-code-qc` mean `~/tools/millennium-hq/plugin/scripts/millennium-code-qc`.
- `$ARGUMENTS` means the request that came with the skill.
- `/millennium:<skill>` means the skill `<skill>`.
- An agent named `millennium:<agent>` is defined in
  `~/tools/millennium-hq/plugin/agents/<agent>.md`: start a subagent with
  that file's instructions and the skills its `skills:` list names. Its
  `tools` line names Claude Code tools: Read, Edit, Write, Bash, Grep, Glob,
  WebFetch, WebSearch. SendMessage means continuing that same agent.
- Before saying work is done, run
  `millennium-code-qc gate --tier full --require-config` and fix every finding.
  Never edit `.millennium/` or work around a gate.
```

Keep it outside the markers of the section `config apply` writes into
`AGENTS.md` (step 4): that section lists the repository's gates and limits,
and `config apply` replaces it on every run.

Alternatively, set `CLAUDE_PLUGIN_ROOT` to the clone's `plugin/` directory in
the environment you start the harness from, so the skills' commands run as
written. Some harnesses do not pass your environment to the agent's shell,
so the `AGENTS.md` note is the reliable option.

### 4. Opt the repository in and work

Invoke `adopt-project` (existing code) or `init-project` (new project) the
way your harness invokes skills, then apply the proposal yourself in a
terminal:

```
millennium-code-qc config apply
```

It also writes Claude Code's settings into `.claude/settings.json`. Other
harnesses ignore them, and they help any teammate who uses Claude Code.

The frontmatter fields that only Claude Code knows are ignored elsewhere.
`disable-model-invocation` normally keeps the agent from starting
`init-project` and `adopt-project` on its own, and `user-invocable: false`
hides the agents' skills from your menu; your harness may do neither.

### Agents without Claude Code

The tech lead's value comes from fresh context: reviewers see the spec and
the diff, never the author's reasoning.

- **If your harness has subagents**, create one per file in
  `plugin/agents/`, using its body as the instructions and giving it the
  skills its frontmatter lists. Give the software-engineer, correctness-reviewer
  and qa-tester their own git worktree if your harness supports it.
- **If it does not**, run the roles yourself. Start a new session for each
  reviewer, give it the spec and `git diff main...`, and ask it to follow
  `review-protocol` and its review skill (for example `correctness-review`).
  Bring its JSON report back to the tech-lead session. For QA, start a
  session in a fresh checkout and tell it not to read the implementation; no
  guard enforces that here.

### Check before you merge

Without hooks, the gate is yours to run:

```
millennium-code-qc gate --tier full --require-config            # this change, against HEAD
millennium-code-qc gate --tier full --require-config --base main   # a whole branch
```

Exit status 0 means it passed; anything else blocks. Running it in a
pre-commit hook or CI turns it back into enforcement. See
[Commands](cli.md) and [Checks](checks.md).

## Your own engine binary

Set `MILLENNIUM_CODE_QC` to an engine binary's absolute path, and the
launcher runs that instead of downloading. Use this for an engine you built
(`cd code-qc && go build -o millennium-code-qc .`), one your organisation
vetted and mirrors, or machines without access to GitHub. It works the same
in every harness. The variable must be set in the environment the harness
starts from.
