#!/usr/bin/env python3
"""What went wrong across recent Claude Code sessions: tool errors, repeated failing
calls, skill use, and the short messages where a person stepped in.

    python3 mine_sessions.py [--last 30] [--root DIR ...]

Roots default to every ~/.claude*/projects. The output holds real project data: read
it, never paste it into a skill.
"""
import argparse, collections, glob, json, os, re

STEPPED_IN = re.compile(r"\b(no|why|wrong|don'?t|didn'?t|doesn'?t|still|again|forgot|instead|have you|did you|are you sure|stop)\b|\?\?", re.I)


def tidy(text):
    text = re.sub(r"/[\w./~-]+", "<path>", text)
    text = re.sub(r"\b[0-9a-f]{7,}\b", "<hash>", text)
    return re.sub(r"\d+", "N", text)


def text_of(content):
    if isinstance(content, list):
        return " ".join(b.get("text", "") for b in content if isinstance(b, dict))
    return str(content or "")


def mine(path, out):
    calls, last_failed, skill = {}, None, None
    with open(path, errors="ignore") as lines:
        for line in lines:
            try:
                row = json.loads(line)
            except ValueError:
                continue
            content = (row.get("message") or {}).get("content")
            if row.get("type") == "assistant" and isinstance(content, list):
                for block in content:
                    if block.get("type") != "tool_use":
                        continue
                    calls[block["id"]] = (block["name"], json.dumps(block.get("input"))[:160])
                    if block["name"] == "Skill":
                        skill = (block.get("input") or {}).get("skill", "?")
                        out["skills"][skill] += 1
                    elif skill:
                        out["calls"][skill] += 1
            if row.get("type") != "user":
                continue
            if isinstance(content, list):
                for block in content:
                    if block.get("type") != "tool_result":
                        continue
                    if not block.get("is_error"):
                        last_failed = None
                        continue
                    name, command = calls.get(block.get("tool_use_id"), ("?", ""))
                    last = [l for l in text_of(block.get("content")).strip().split("\n") if l.strip()]
                    out["errors"][f"{name}: {tidy(last[-1] if last else '')[:110]}"] += 1
                    if (name, command) == last_failed:
                        out["retries"][name] += 1
                    last_failed = (name, command)
            said = text_of(content).strip()
            if said and len(said) < 400 and not said.startswith("<") and not row.get("isMeta") and STEPPED_IN.search(said):
                out["stepped_in"][skill or "-"].append(said.replace("\n", " ")[:170])


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--last", type=int, default=30, help="newest sessions per root")
    parser.add_argument("--root", action="append")
    args = parser.parse_args()
    roots = args.root or glob.glob(os.path.expanduser("~/.claude*/projects"))
    out = {"errors": collections.Counter(), "retries": collections.Counter(), "skills": collections.Counter(), "calls": collections.Counter(), "stepped_in": collections.defaultdict(list)}
    for root in roots:
        files = sorted((f for f in glob.glob(f"{root}/*/*.jsonl") if os.path.getsize(f) > 20000), key=os.path.getmtime, reverse=True)
        for path in files[: args.last]:
            mine(path, out)
    print("== errors, most common first")
    for sig, n in out["errors"].most_common(40):
        print(f"{n:4} {sig}")
    print("== the same failing call sent again:", dict(out["retries"]))
    print("== skills: uses, tool calls after")
    for name, n in out["skills"].most_common(25):
        print(f"{n:4} {name} ({out['calls'][name]} calls)")
    print("== where a person stepped in")
    for name, said in out["stepped_in"].items():
        print(f"-- {name}")
        for line in list(dict.fromkeys(said))[:12]:
            print(f"   {line}")


if __name__ == "__main__":
    main()
