"""Music bed from a loop library plus UI sounds on cue, written as one 48 kHz stereo wav.

python3 soundtrack.py list <loop root> [subfolder ...]
python3 soundtrack.py mix <spec.json> <out.wav>

spec.json:
  {"duration": 98, "bpm": 116, "root": "/Library/Audio/Apple Loops/Apple",
   "loops": {"pad": "<sub>/<name>", "bass": "...", "beat": "...", "lead": "...", "extra": "..."},
   "cues": [[1.2, "pop", 0.8], [3.0, "typing", 0.6, 4.4], [9.5, "success", 1]]}

Loops are paths under root without the extension; pad is required, the rest optional.
Cue sounds: tap tick pop bubble whoosh chime success alert hit swell heart beep, and
typing (a tick every 75 ms until the fourth value). Needs ffmpeg, numpy, soundfile, scipy.
"""
import json
import os
import struct
import subprocess
import sys
import tempfile

import numpy as np
import soundfile as sf
from scipy.signal import butter, sosfilt

SR = 48000
EXT = (".caf", ".aif", ".aiff", ".wav", ".m4a")
GAIN = dict(pad=0.8, bass=0.75, beat=0.7, lead=0.55, extra=0.45)
rng = np.random.default_rng(7)


def caf_info(path):
    with open(path, "rb") as f:
        data = f.read()
    out, i = {}, 8
    while i + 12 <= len(data):
        kind, size = data[i:i + 4], struct.unpack(">q", data[i + 4:i + 12])[0]
        if size < 0:
            break
        if kind == b"info":
            parts = data[i + 16:i + 12 + size].split(b"\x00")
            out.update({k.decode(errors="ignore"): v.decode(errors="ignore") for k, v in zip(parts[::2], parts[1::2])})
        i += 12 + size
    return out


def seconds(path):
    r = subprocess.run(["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path],
                       capture_output=True, text=True)
    try:
        return float(r.stdout.strip())
    except ValueError:
        return 0.0


def guess_bpm(dur):
    for beats in (4, 8, 16, 32, 64):
        bpm = beats * 60 / dur
        if 78 <= bpm <= 135:
            return round(bpm, 1)
    return ""


def list_loops(root, subs):
    for sub in subs or sorted(os.listdir(root)):
        folder = os.path.join(root, sub)
        if not os.path.isdir(folder):
            continue
        for fn in sorted(os.listdir(folder)):
            if not fn.lower().endswith(EXT):
                continue
            path = os.path.join(folder, fn)
            meta = caf_info(path) if fn.lower().endswith(".caf") else {}
            dur = seconds(path)
            print(f"{sub[:16]:16} | {os.path.splitext(fn)[0][:40]:40} | {meta.get('key signature', ''):3} | "
                  f"{(meta.get('category') or meta.get('genre', ''))[:12]:12} | {guess_bpm(dur) if dur else ''} | {dur:.2f}s")


def load(root, rel):
    src = next((os.path.join(root, rel + e) for e in EXT if os.path.exists(os.path.join(root, rel + e))), None)
    if not src:
        sys.exit(f"loop not found: {rel}")
    with tempfile.NamedTemporaryFile(suffix=".wav") as tmp:
        subprocess.run(["ffmpeg", "-v", "error", "-y", "-i", src, "-ar", str(SR), "-ac", "2", tmp.name], check=True)
        x, _ = sf.read(tmp.name)
    return x


def tile(loop, bars, bar):
    per = max(1, round(len(loop) / bar))
    clean = loop[:per * bar]
    return np.concatenate([clean] * int(np.ceil(bars / per)))[:bars * bar]


def bed(spec):
    loops, seconds_ = spec["loops"], spec["duration"]
    bar = int(round(4 * 60 / spec["bpm"] * SR))
    nbars = int(np.ceil(seconds_ * SR / bar)) + 1
    plan = {k: np.zeros(nbars, dtype=bool) for k in GAIN}
    intro = outro = 4
    for b in range(nbars):
        cyc = (b - intro) % 24
        plan["pad"][b] = True
        if b < intro or b >= nbars - outro:
            continue
        brk = 16 <= cyc < 20
        plan["bass"][b] = not brk
        plan["beat"][b] = not brk and cyc >= 4
        plan["lead"][b] = cyc >= 8 or brk
        plan["extra"][b] = 20 <= cyc < 24
    mix = np.zeros((nbars * bar, 2))
    ramp = np.ones(int(0.03 * SR)) / int(0.03 * SR)
    for k, on in plan.items():
        if k not in loops or not on.any():
            continue
        layer = tile(load(spec["root"], loops[k]), nbars, bar)
        mask = np.convolve(np.repeat(on.astype(float), bar), ramp, mode="same")
        mix[:len(layer)] += layer * mask[:len(layer), None] * GAIN[k]
    n = int(seconds_ * SR)
    mix = mix[:n]
    fi, fo = int(1.2 * SR), int(4.0 * SR)
    mix[:fi] *= np.linspace(0, 1, fi)[:, None]
    mix[n - fo:] *= (np.linspace(1, 0, fo) ** 1.4)[:, None]
    return mix / (np.sqrt(np.mean(mix ** 2)) + 1e-9) * 10 ** (-19 / 20)


def env(n, a, d):
    t = np.arange(n) / SR
    return np.minimum(1, t / max(a, 1e-4)) * np.exp(-t / d)


def sine(f, dur, a=0.002, d=0.1, f2=None):
    n = int(dur * SR)
    freq = np.full(n, f) if f2 is None else np.geomspace(f, f2, n)
    return np.sin(2 * np.pi * np.cumsum(freq) / SR) * env(n, a, d)


def band(x, lo, hi):
    return sosfilt(butter(2, [lo, hi], btype="band", fs=SR, output="sos"), x)


def lay(*parts):
    out = np.zeros(max(len(p) for p in parts))
    for p in parts:
        out[:len(p)] += p
    return out


def at(offsets):
    return lay(*[np.concatenate([np.zeros(int(t * SR)), s]) for t, s in offsets])


def whoosh(dur=0.55):
    n = int(dur * SR)
    x = rng.standard_normal(n)
    bands = [(300, 900), (600, 1800), (1200, 3600), (2400, 7000)]
    return 0.8 * sum(band(x, lo, hi) * np.sin(np.linspace(0, np.pi, n)) ** (1.5 + i * 0.5) * (0.35 - i * 0.05)
                     for i, (lo, hi) in enumerate(bands))


def hit():
    n = int(1.6 * SR)
    t = np.arange(n) / SR
    sub = np.sin(2 * np.pi * np.cumsum(38 + 70 * np.exp(-t * 18)) / SR) * np.exp(-t * 2.6)
    return 0.9 * sub + band(rng.standard_normal(n), 2500, 9000) * np.exp(-t * 5) * 0.08


def swell(dur=1.6):
    n = int(dur * SR)
    t = np.linspace(0, 1, n)
    x = rng.standard_normal(n)
    return (band(x, 400, 2400) * (1 - t) * 0.12 + band(x, 2400, 9000) * t * 0.14) * t ** 2


SOUNDS = dict(
    tap=lambda: lay(0.55 * sine(2400, 0.05, 0.0005, 0.012), 0.25 * band(rng.standard_normal(int(0.03 * SR)), 2000, 8000) * env(int(0.03 * SR), 0.0003, 0.006)),
    tick=lambda: 0.35 * sine(3200, 0.04, 0.0005, 0.008),
    pop=lambda: 0.6 * sine(520, 0.12, 0.002, 0.035, f2=1250),
    bubble=lambda: lay(0.5 * sine(380, 0.18, 0.004, 0.05, f2=900), 0.2 * sine(1400, 0.08, 0.001, 0.02)),
    whoosh=whoosh,
    chime=lambda: lay(0.32 * sine(1318.5, 1.2, 0.003, 0.35), 0.22 * sine(1975.5, 1.0, 0.003, 0.28), 0.08 * sine(2637, 0.8, 0.002, 0.2)),
    success=lambda: at([(i * 0.07, 0.22 * sine(f, 0.8, 0.002, 0.22)) for i, f in enumerate([1046.5, 1318.5, 1568, 2093])]),
    alert=lambda: lay(0.3 * sine(740, 0.25, 0.003, 0.09), 0.2 * sine(622, 0.3, 0.003, 0.12)),
    hit=hit,
    swell=swell,
    heart=lambda: 0.4 * sine(880, 0.15, 0.002, 0.05, f2=1320),
    beep=lambda: at([(0, 0.3 * sine(1760, 0.09, 0.002, 0.05)), (0.17, 0.3 * sine(1760, 0.09, 0.002, 0.05))]),
)


def expand(cues):
    for c in cues:
        if c[1] == "typing":
            yield from ((t, "tick", c[2]) for t in np.arange(c[0], c[3], 0.075))
        else:
            yield c[0], c[1], c[2]


def mix(spec_path, out):
    with open(spec_path) as f:
        spec = json.load(f)
    music = bed(spec)
    n = len(music)
    fx = np.zeros(n)
    for t, kind, g in expand(spec.get("cues", [])):
        s, i = SOUNDS[kind](), int(t * SR)
        if i < n:
            j = min(n, i + len(s))
            fx[i:j] += g * s[:j - i]
    fx *= 10 ** (-10 / 20) / (np.abs(fx).max() + 1e-9)
    right = np.concatenate([np.zeros(int(0.0004 * SR)), fx])[:n]
    st = np.stack([music[:, 0] + fx * 0.92, music[:, 1] + right * 0.92], axis=1)
    st = np.tanh(st * 1.15) / np.tanh(1.15)
    st *= 10 ** (-1 / 20) / (np.abs(st).max() + 1e-9)
    sf.write(out, st.astype(np.float32), SR, subtype="PCM_24")
    print(f"{spec['duration']:.0f}s -> {out}")


if __name__ == "__main__":
    if len(sys.argv) >= 3 and sys.argv[1] == "list":
        list_loops(sys.argv[2], sys.argv[3:])
    elif len(sys.argv) == 4 and sys.argv[1] == "mix":
        mix(sys.argv[2], sys.argv[3])
    else:
        sys.exit(__doc__)
