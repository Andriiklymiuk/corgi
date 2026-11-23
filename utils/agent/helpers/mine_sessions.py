#!/usr/bin/env python3
"""What went wrong across recent Claude Code sessions: tool errors, repeated failing
calls, skill use, and each place where a person stepped in, with what the agent did
before it and what it answered.

    python3 mine_sessions.py [--last 30] [--days 7] [--root DIR ...] [--project DIR ...]
    python3 mine_sessions.py --show <session>:<line> [--around 15]

Roots default to every ~/.claude*/projects. Only sessions a person typed in are read:
headless runs (unattended runs, evals, scripts) are counted and left out. The output
holds real project data: read it, never paste it into a skill.
"""
import argparse, collections, glob, json, os, re, sys, time

STEPPED_IN = re.compile(r"\b(no|why|wrong|don'?t|didn'?t|doesn'?t|still|again|forgot|instead|have you|did you|are you sure|stop)\b|\?\?", re.I)
NOT_A_PERSON = re.compile(r"^(<|\[Request interrupted|\[Image|Base directory for this skill|This session is being continued|\[SYSTEM NOTIFICATION)")
EXCHANGES = 25


def tidy(text):
    text = re.sub(r"/[\w./~-]+", "<path>", text)
    text = re.sub(r"\b[0-9a-f]{7,}\b", "<hash>", text)
    return re.sub(r"\d+", "N", text)


def text_of(content):
    if isinstance(content, list):
        return " ".join(b.get("text", "") for b in content if isinstance(b, dict))
    return str(content or "")


def one_line(text, limit):
    text = " ".join(str(text).split())
    return text if len(text) <= limit else text[:limit] + "…"


def rows_of(path):
    with open(path, errors="ignore") as lines:
        for number, line in enumerate(lines):
            try:
                row = json.loads(line)
            except ValueError:
                continue
            if isinstance(row, dict):
                yield number, row


def said_by_person(row):
    attachment = row.get("attachment") or {}
    if attachment.get("type") == "queued_command":
        human = (attachment.get("origin") or {}).get("kind") in (None, "human")
        return text_of(attachment.get("prompt")).strip() if human and attachment.get("commandMode") in (None, "prompt") else ""
    if row.get("type") != "user" or row.get("isMeta") or row.get("isSidechain"):
        return ""
    said = text_of((row.get("message") or {}).get("content")).strip()
    return "" if NOT_A_PERSON.match(said) else said


def headless(path):
    for _, row in rows_of(path):
        if row.get("entrypoint"):
            return row["entrypoint"].startswith("sdk")
    return False


def did(block):
    if block.get("type") == "text":
        return block.get("text", "").strip()
    if block.get("type") == "tool_use":
        given = block.get("input") or {}
        return f"[{block.get('name')}] " + str(given.get("description") or given.get("skill") or given.get("file_path") or given.get("command") or "")
    return ""


def mine(path, out):
    calls, last_failed, skill = {}, None, None
    session = os.path.basename(path)[:8]
    before, open_exchange = "", None
    for number, row in rows_of(path):
        content = (row.get("message") or {}).get("content")
        if row.get("type") == "assistant" and isinstance(content, list) and not row.get("isSidechain"):
            for block in content:
                words = did(block)
                if words:
                    before = words
                if open_exchange and block.get("type") == "text" and words:
                    open_exchange["after"] = words
                    open_exchange = None
                if block.get("type") != "tool_use":
                    continue
                calls[block["id"]] = (block["name"], json.dumps(block.get("input"))[:160])
                if block["name"] == "Skill":
                    skill = (block.get("input") or {}).get("skill", "?")
                    out["skills"][skill] += 1
                elif skill:
                    out["calls"][skill] += 1
        if row.get("type") == "user" and isinstance(content, list):
            for block in content:
                if not isinstance(block, dict) or block.get("type") != "tool_result":
                    continue
                if not block.get("is_error"):
                    last_failed = None
                    continue
                name, command = calls.get(block.get("tool_use_id"), ("?", ""))
                last = [l for l in text_of(block.get("content")).strip().split("\n") if l.strip()]
                out["errors"][f"{name}: {tidy(last[-1] if last else '')[:110]}"].append(session)
                if (name, command) == last_failed:
                    out["retries"][name] += 1
                last_failed = (name, command)
        said = said_by_person(row)
        if said and len(said) < 400 and STEPPED_IN.search(said):
            if out["exchanges"] and out["exchanges"][-1]["said"] == said and out["exchanges"][-1]["session"] == session:
                continue
            open_exchange = {"session": session, "line": number, "at": str(row.get("timestamp", ""))[:16], "skill": skill or "-", "before": before, "said": said, "after": ""}
            out["exchanges"].append(open_exchange)


def in_project(path, wanted):
    if not wanted:
        return True
    folder = os.path.basename(os.path.dirname(path))
    return any(folder == w or folder.startswith(w + "-") for w in wanted)


def transcripts(args):
    roots = [os.path.expanduser(r) for r in args.root] if args.root else glob.glob(os.path.expanduser("~/.claude*/projects"))
    wanted = [re.sub(r"[^A-Za-z0-9]", "-", os.path.abspath(os.path.expanduser(p))) for p in args.project or []]
    for root in roots:
        yield sorted((f for f in glob.glob(f"{root}/*/*.jsonl") if in_project(f, wanted)), key=os.path.getmtime, reverse=True)


def show(args):
    session, _, line = args.show.partition(":")
    found = [f for files in transcripts(args) for f in files if os.path.basename(f).startswith(session)] if session else []
    if not found:
        sys.exit(f"no session {session!r} under these roots")
    if not line.isdigit():
        sys.exit("--show wants <session>:<line>, as the list prints it")
    for number, row in rows_of(found[0]):
        if abs(number - int(line)) > args.around:
            continue
        said = said_by_person(row)
        if said:
            print(f"{number} PERSON: {one_line(said, 1500)}")
            continue
        content = (row.get("message") or {}).get("content")
        if not isinstance(content, list):
            continue
        for block in content:
            if not isinstance(block, dict):
                continue
            if block.get("type") == "tool_result":
                print(f"{number}   result: {one_line(text_of(block.get('content')), 300)}")
            elif row.get("type") == "assistant" and did(block):
                print(f"{number} AGENT: {one_line(did(block), 900)}")


def report(args):
    out = {"errors": collections.defaultdict(list), "retries": collections.Counter(), "skills": collections.Counter(), "calls": collections.Counter(), "exchanges": []}
    read, left_out, oldest = 0, 0, None
    since = time.time() - args.days * 86400 if args.days else 0
    for files in transcripts(args):
        taken = 0
        for path in files:
            changed = os.path.getmtime(path)
            if taken == args.last or changed < since:
                break
            if os.path.getsize(path) <= 20000:
                continue
            if headless(path):
                left_out += 1
                continue
            mine(path, out)
            taken += 1
            oldest = min(oldest or changed, changed)
        read += taken
    print(f"== read {read} sessions a person typed in" + (f", back to {time.strftime('%Y-%m-%d', time.localtime(oldest))}" if oldest else "") + f"; left out {left_out} headless runs")
    print("== errors, most common first (how many, in how many sessions)")
    for sig, where in sorted(out["errors"].items(), key=lambda e: len(e[1]), reverse=True)[:40]:
        print(f"{len(where):4} in {len(set(where))}  {sig}")
    print("== the same failing call sent again:", dict(out["retries"]))
    print("== skills: uses, tool calls after")
    for name, n in out["skills"].most_common(25):
        print(f"{n:4} {name} ({out['calls'][name]} calls)")
    print("== where a person may have stepped in, newest first. A question is not a correction: read 'after'")
    for x in sorted(out["exchanges"], key=lambda x: x["at"], reverse=True)[:EXCHANGES]:
        print(f"-- {x['session']}:{x['line']}  {x['at']}  {x['skill']}")
        print(f"   before: {one_line(x['before'], 220)}")
        print(f"   person: {one_line(x['said'], 400)}")
        print(f"   after:  {one_line(x['after'] or '(no answer in words)', 320)}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--last", type=int, default=30, help="newest sessions per root")
    parser.add_argument("--days", type=int, default=0, help="only sessions touched in this many days")
    parser.add_argument("--root", action="append", help="a Claude account's projects folder, e.g. ~/.claude-work/projects")
    parser.add_argument("--project", action="append", help="only sessions run in this folder or below it")
    parser.add_argument("--show", help="<session>:<line> - the exchange around one line the list printed")
    parser.add_argument("--around", type=int, default=15, help="with --show: how many rows on each side")
    args = parser.parse_args()
    if args.show:
        show(args)
    else:
        report(args)


if __name__ == "__main__":
    main()
