# NeuraBox

**An airlock for AI-generated code.**

AI coding agents write code fast. Reviewing it safely is slow. Scanners flag problems. Bots leave comments. Nothing actually stops non-compliant code from landing.

NeuraBox sits *before* the code touches your project — sandboxes the agent, enforces your policy, and only exports code that passes.

> **Not a scanner. Not a reviewer. A gate.**

---

## Demo

[Watch the 2-minute demo](https://youtu.be/GQigqCTQTzA)

---

## Quickstart

### 1. Install

```bash
npm install -g neurabox
```

### 2. Initialize a policy

```bash
neurabox --init
```

### 3. Run your agent

```bash
neurabox claude          # Claude Code
neurabox gemini          # Gemini CLI
neurabox aider           # Aider
neurabox python agent.py # Any agent/script
```

NeuraBox creates an isolated shadow workspace, injects a pre‑digested project context (architecture + dependencies), runs your agent, enforces policy checks, and asks for approval before exporting any changes.

---

## Why NeuraBox?

| Approach | Problem |
|----------|---------|
| **Git worktrees** | No policy, no audit, no gate |
| **Hooks/scripts** | Agent governs itself |
| **PR bots** | Advisory only, after code lands |
| **Scanners** | Find issues in code that already exists |

NeuraBox **blocks** before code exists in your codebase.

---

## How it works

1. **Native OS sandbox** — no Docker required (Windows Job Objects, Linux `setpgid`, macOS process groups)
2. **Token optimization** — intercepts `git`/`npm`/`npx` to save 60–90% tokens
3. **Project graph** — AST‑based context (architecture, dependencies, cross‑file connections)
4. **System directives** — Enforces terse AI responses to generate less bloated code.
5. **Policy enforcement** — your rules, your approval

---

## Default policy (`nb-policy.yaml`)

```yaml
blocks:
  - ".env"
  - "node_modules"

checks:
  - cname: "structure"
    command: "[ -d 'src/controllers' ]"
  - cname: "no-internet"
    command: "curl -m 2 google.com || echo 'Safe'"
```

- `blocks` – files never exported
- `checks` – shell commands that must pass

---

## Audit log

Every session is logged to `audit.log`:

## Privacy

- No code, prompts, or files sent to any server except your AI provider.
- One anonymous ping per session to count unique users

---

## What's next?

- Encrypted audit logs
- Team policy sharing
- Hardware isolation

---

## Feedback


Reach out: `tandicksen@gmail.com` or open an issue.

---

## OSS

- RTK (https://github.com/rtk-ai/rtk)
- GRAPHIFY (Modified) (https://github.com/safishamsi/graphify)
- CAVEMAN (https://github.com/juliusbrussee/caveman)
- Ponytail (https://github.com/DietrichGebert/ponytail)

---

**Package:** [`neurabox` on npm](https://www.npmjs.com/package/neurabox)

**OSS:**

**License:** [Apache 2.0](LICENSE)
```

---