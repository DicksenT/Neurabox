"""neuragraph_ast.py — neurabox context generator (Pass 1 only, no LLM calls)

Usage:
    python neuragraph_ast.py <project_dir> <shadow_dir> [--audit-log <path>] [--changed <path>] [--debug]

Writes into shadow_dir:
    graph.json      — full node-link graph (agent can query for detail)
    CLAUDE.md       — pre-digested context for Claude Code
    GEMINI.md       — pre-digested context for Gemini CLI
    .aider.md       — pre-digested context for Aider
    AI_CONTEXT.md   — fallback for any other agent

The pre-digested files are ~1800 tokens each. The agent reads one on startup
instead of exploring the codebase from scratch.
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import pickle
from pathlib import Path
from datetime import datetime
from neuragraph.extract import extract, collect_files as extract_collect
from neuragraph.build import build_from_json


# 🛡️ PYINSTALLER HIDDEN IMPORTS FIX
# extract.py loads these dynamically, which PyInstaller cannot see.
# We explicitly import them here so they are compiled into the binary.
try:
    import tree_sitter_python
    import tree_sitter_javascript
    import tree_sitter_typescript
    import tree_sitter_go
    import tree_sitter_rust
    import tree_sitter_java
    import tree_sitter_groovy
    import tree_sitter_c
    import tree_sitter_cpp
    import tree_sitter_ruby
    import tree_sitter_c_sharp
    import tree_sitter_kotlin
    import tree_sitter_scala
    import tree_sitter_php
    import tree_sitter_lua
    import tree_sitter_zig
    import tree_sitter_powershell
    import tree_sitter_elixir
    import tree_sitter_objc
    import tree_sitter_julia
    import tree_sitter_verilog
    import tree_sitter_fortran
    import tree_sitter_bash
    import tree_sitter_json
    import tree_sitter_dm
    import tree_sitter_sql
except ImportError:
    pass

# 🚀 FIX: MOVED OUT OF THE FUNCTION SO PYINSTALLER CAN SEE THEM

# ---------------------------------------------------------------------------
# neuragraph pipeline (Pass 1 only)
# ---------------------------------------------------------------------------

def _build_graph(project_dir: Path, debug: bool):
    files = extract_collect(project_dir)
    if debug:
        print(f"[neuragraph_ast] detected {len(files)} code files", file=sys.stderr)

    if not files:
        return None, None

    # --- PERSISTENT CACHE (mtime based) ---
    cache_dir = project_dir / "nb-graph"
    cache_dir.mkdir(exist_ok=True)
    mtime_cache_file = cache_dir / "mtime_cache.pkl"
    graph_cache_file = cache_dir / "graph_cached.pkl"

    # Load previous mtime cache
    mtime_cache = {}
    if mtime_cache_file.exists():
        try:
            with open(mtime_cache_file, 'rb') as f:
                mtime_cache = pickle.load(f)
        except Exception:
            pass

    # Check which files changed
    changed_files = []
    for f in files:
        key = str(f.resolve())
        current_mtime = f.stat().st_mtime
        if key not in mtime_cache or mtime_cache[key] != current_mtime:
            changed_files.append(f)

    if not changed_files and graph_cache_file.exists():
        # No changes – load cached graph
        if debug:
            print("[neuragraph_ast] loading cached graph (no file changes)", file=sys.stderr)
        with open(graph_cache_file, 'rb') as f:
            G = pickle.load(f)
        # We still need graph_data for JSON output
        graph_data = {
            "nodes": [{"id": n, **d} for n, d in G.nodes(data=True)],
            "edges": [{"source": u, "target": v, **d} for u, v, d in G.edges(data=True)],
        }
        return G, graph_data

    # --- Extract only changed files? ---
    # For simplicity, we re‑extract everything. But we could implement incremental extraction.
    # Given that typical projects have few changes between sessions, re‑extracting all is fine.
    if debug and changed_files:
        print(f"[neuragraph_ast] {len(changed_files)} files changed, rebuilding graph", file=sys.stderr)

    # 🛡️ CACHE FIX: Intercept Go's APPDATA environment variable
    cache_env = os.environ.get("GRAPHIFY_OUT")
    cache_root = Path(cache_env).resolve() if cache_env else project_dir

    result = extract(files, cache_root=cache_root, parallel=False)
    
    if debug:
        print(f"[neuragraph_ast] extracted {len(result.get('nodes',[]))} nodes, "
              f"{len(result.get('edges',[]))} edges", file=sys.stderr)

    G = build_from_json(result, directed=False, root=project_dir)

    # Save updated mtime cache
    for f in files:
        key = str(f.resolve())
        mtime_cache[key] = f.stat().st_mtime
    with open(mtime_cache_file, 'wb') as f:
        pickle.dump(mtime_cache, f)

    # Save graph pickle for next run
    with open(graph_cache_file, 'wb') as f:
        pickle.dump(G, f)

    graph_data = {
        "nodes": [{"id": n, **d} for n, d in G.nodes(data=True)],
        "edges": [{"source": u, "target": v, **d} for u, v, d in G.edges(data=True)],
    }
    return G, graph_data

# ---------------------------------------------------------------------------
# Analysis helpers (no dependency on neuragraph.analyze — keeps this standalone)
# ---------------------------------------------------------------------------

def _god_nodes(G, top_n: int = 10) -> list[dict]:
    """Return top N nodes by degree."""
    import networkx as nx
    scored = sorted(
        ({"id": n, "label": G.nodes[n].get("label", n),
          "degree": G.degree(n),
          "source_file": G.nodes[n].get("source_file", "")}
         for n in G.nodes()),
        key=lambda x: -x["degree"],
    )
    return [s for s in scored if s["degree"] > 0][:top_n]


def _communities(G, max_communities: int = 8) -> list[dict]:
    """Simple Louvain community detection. Returns list of {label, nodes}."""
    import networkx as nx
    if G.number_of_edges() == 0:
        return []
    try:
        import inspect
        kwargs: dict = {"seed": 42, "threshold": 1e-4}
        if "max_level" in inspect.signature(nx.community.louvain_communities).parameters:
            kwargs["max_level"] = 10
        comms = nx.community.louvain_communities(G.to_undirected(), **kwargs)
    except Exception:
        return []

    result = []
    for nodes in sorted(comms, key=len, reverse=True)[:max_communities]:
        # Label the community after its highest-degree node
        top = max(nodes, key=lambda n: G.degree(n))
        label = G.nodes[top].get("label", top)
        member_labels = sorted(
            G.nodes[n].get("label", n) for n in nodes
        )[:6]
        result.append({"label": label, "size": len(nodes), "members": member_labels})
    return result


def _surprises(G, god_ids: set, top_n: int = 5) -> list[dict]:
    """Cross-file edges between nodes in different source files, excluding god nodes."""
    import networkx as nx
    candidates = []
    for u, v, d in G.edges(data=True):
        if u in god_ids or v in god_ids:
            continue
        sf_u = G.nodes[u].get("source_file", "")
        sf_v = G.nodes[v].get("source_file", "")
        if sf_u and sf_v and sf_u != sf_v:
            candidates.append({
                "source": G.nodes[u].get("label", u),
                "target": G.nodes[v].get("label", v),
                "relation": d.get("relation", "related_to"),
                "source_file": Path(sf_u).name,
                "target_file": Path(sf_v).name,
            })
    # Sort by relation specificity — "calls" > "imports" > generic
    priority = {"calls": 0, "implements": 1, "uses": 2, "imports": 3}
    candidates.sort(key=lambda x: priority.get(x["relation"], 9))
    return candidates[:top_n]


# ---------------------------------------------------------------------------
# Audit log reader
# ---------------------------------------------------------------------------

def _read_audit_log(path: str | None, max_entries: int = 3) -> list[dict]:
    if not path:
        return []
    p = Path(path)
    if not p.exists():
        return []
    entries = []
    try:
        for line in reversed(p.read_text(encoding="utf-8", errors="ignore").splitlines()):
            line = line.strip()
            if not line:
                continue
            try:
                entries.append(json.loads(line))
            except json.JSONDecodeError:
                pass
            if len(entries) >= max_entries:
                break
    except OSError:
        pass
    return entries


def _read_changed_files(path: str | None) -> list[str]:
    if not path or not Path(path).exists():
        return []
    return [l.strip() for l in Path(path).read_text(encoding="utf-8").splitlines() if l.strip()]


# ---------------------------------------------------------------------------
# Renderer
# ---------------------------------------------------------------------------

_BUDGET = 8_000  # ~2000 tokens at 4 chars/token


def _render(
    project_dir: Path,
    G,
    audit_log_path: str | None,
    changed_files: list[str],
) -> str:
    now = datetime.utcnow().strftime("%Y-%m-%d %H:%M UTC")
    name = project_dir.name

    gods = _god_nodes(G)
    god_ids = {g["id"] for g in gods}
    comms = _communities(G)
    surprises = _surprises(G, god_ids)
    audit_entries = _read_audit_log(audit_log_path)

    lines = [
        f"# {name} — Project Context",
        f"> Auto-generated {now} by neurabox. Do not edit.",
        f"> {G.number_of_nodes()} nodes · {G.number_of_edges()} edges"
        f" · {len(comms)} communities · full graph at `graph.json`",
        "",
        # 🚀 INJECTED CAVEMAN DIRECTIVE (FULL INTENSITY)
        "## 🛑 OUTPUT RULES (CAVEMAN MODE)",
        "> ACTIVE EVERY RESPONSE. No revert. No filler drift.",
        "",
        "Respond terse like smart caveman. All technical substance stay. Only fluff die.",
        "- Drop: articles (a/an/the), filler (just/really/basically/actually/simply), pleasantries.",
        "- Fragments OK. Short synonyms (big not extensive, fix not 'implement a solution for').",
        "- Technical terms exact. Code blocks unchanged. Errors quoted exact.",
        "- Pattern: `[thing] [action] [reason]. [next step].`",
        "- Not: 'Sure! I'd be happy to help you with that. The issue is likely caused by...'",
        "- Yes: 'Bug in auth middleware. Token expiry check use `<` not `<=`. Fix:'",
        "",
        "### Auto-Clarity Override:",
        "- Drop caveman ONLY for security warnings, irreversible action confirmations, or multi-step destructive sequences.",
        "- Resume caveman immediately after clear part done.",
        "",
        "---",
        ""
    ]

    # README first paragraph
    for readme_name in ("README.md", "README.rst", "README.txt"):
        p = project_dir / readme_name
        if p.exists():
            try:
                first = p.read_text(encoding="utf-8", errors="ignore").split("\n\n")[0]
                first = first.replace("#", "").strip()
                if first:
                    lines += ["## What this project does", first[:400], ""]
            except OSError:
                pass
            break

    # Architecture
    if comms:
        lines.append("## Architecture")
        for c in comms:
            members = ", ".join(f"`{m}`" for m in c["members"])
            suffix = f" +{c['size'] - len(c['members'])} more" if c["size"] > len(c["members"]) else ""
            lines.append(f"- **{c['label']}** ({c['size']} nodes): {members}{suffix}")
        lines.append("")

    # God nodes
    if gods:
        lines.append("## Core abstractions (most connected — read these files first)")
        for g in gods:
            src = Path(g["source_file"]).name if g["source_file"] else ""
            loc = f" — `{src}`" if src else ""
            lines.append(f"- `{g['label']}` ({g['degree']} connections){loc}")
        lines.append("")

    # Session history
    lines.append("## Recent session history")
    if audit_entries:
        for e in audit_entries:
            prompt = (e.get("Prompt") or e.get("prompt") or "")[:80]
            files = e.get("Files") or e.get("files") or []
            approved = e.get("Approved") or e.get("approved") or False
            agent = e.get("Agent") or e.get("agent") or "unknown"
            status = "✓ exported" if approved else "✗ discarded"
            files_str = ", ".join(str(f) for f in files[:4])
            if len(files) > 4:
                files_str += f" +{len(files)-4} more"
            lines.append(f"- **{agent}** [{status}]{': _' + prompt + '_' if prompt else ''}")
            if files_str:
                lines.append(f"  Changed: `{files_str}`")
    else:
        lines.append("_No previous sessions._")
    lines.append("")

    # Changed files
    lines.append("## Changed since last session")
    if changed_files:
        for f in changed_files[:15]:
            lines.append(f"- `{f}`")
        if len(changed_files) > 15:
            lines.append(f"_...and {len(changed_files) - 15} more_")
    else:
        lines.append("_No changes detected or first session._")
    lines.append("")

    # Surprising connections
    if surprises:
        lines.append("## Surprising connections (cross-file — worth knowing)")
        for s in surprises:
            lines.append(
                f"- `{s['source']}` ({s['source_file']}) "
                f"--{s['relation']}--> "
                f"`{s['target']}` ({s['target_file']})"
            )
        lines.append("")

    result = "\n".join(lines)
    if len(result) > _BUDGET:
        result = result[:_BUDGET] + "\n\n_Context truncated to fit token budget._\n"
    return result


# ---------------------------------------------------------------------------
# Agent file names
# ---------------------------------------------------------------------------

_AGENT_FILES = {
    "claude":  "CLAUDE.md",
    "gemini":  "GEMINI.md",
    "aider":   ".aider.md",
    "cursor":  ".cursorrules",
}
_FALLBACK_FILE = "AI_CONTEXT.md"


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main() -> None:
    import multiprocessing
    multiprocessing.freeze_support()

    parser = argparse.ArgumentParser(prog="neuragraph_ast")
    parser.add_argument("project_dir")
    parser.add_argument("shadow_dir")
    parser.add_argument("--audit-log", default=None)
    parser.add_argument("--changed", default=None)
    parser.add_argument("--debug", action="store_true")
    args = parser.parse_args()

    project_dir = Path(args.project_dir).resolve()
    shadow_dir = Path(args.shadow_dir).resolve()

    G, graph_data = _build_graph(project_dir, args.debug)
    if G is None:
        if args.debug:
            print("[neuragraph_ast] no files found, skipping", file=sys.stderr)
        sys.exit(0)

    # 🚀 FIX: Handle both directory paths and direct file targets safely
    if shadow_dir.suffix == '.json':
        graph_path = shadow_dir
        output_directory = shadow_dir.parent
    else:
        graph_path = shadow_dir / "graph.json"
        output_directory = shadow_dir

    output_directory.mkdir(parents=True, exist_ok=True)
    graph_path.write_text(json.dumps(graph_data, indent=2), encoding="utf-8")

    # Render the pre-digested context
    changed = _read_changed_files(args.changed)
    context = _render(project_dir, G, args.audit_log, changed)

    # Write one file per agent + fallback
    for filename in list(_AGENT_FILES.values()) + [_FALLBACK_FILE]:
        (shadow_dir / filename).write_text(context, encoding="utf-8")

    if args.debug:
        tokens = len(context) // 4
        print(f"[neuragraph_ast] wrote context (~{tokens} tokens) + graph.json", file=sys.stderr)


if __name__ == "__main__":
    main()
