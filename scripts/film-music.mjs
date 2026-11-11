import { execFileSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";

const output = resolve(process.argv[2] ?? "docs/film/music.mp3");
const RATE = 44100;
const BEAT = 60 / 90;
const BAR = BEAT * 4;
const BARS = 68;
const SWING = 0.035;
const total = Math.ceil((BARS * BAR + 1.5) * RATE);

const bus = () => [new Float32Array(total), new Float32Array(total)];
const dry = bus(), wet = bus(), echo = bus();
const hz = (midi) => 440 * Math.pow(2, (midi - 69) / 12);
let noiseSeed = 1234567;
const noise = () => ((noiseSeed = (noiseSeed * 1664525 + 1013904223) >>> 0) / 2147483648) - 1;

function voice(target, start, length, pan, sampleAt) {
	const from = Math.max(0, Math.floor(start * RATE));
	const count = Math.min(total - from, Math.floor(length * RATE));
	const left = Math.cos((pan + 1) * Math.PI / 4), right = Math.sin((pan + 1) * Math.PI / 4);
	for (let i = 0; i < count; i++) {
		const value = sampleAt(i / RATE);
		target[0][from + i] += value * left;
		target[1][from + i] += value * right;
	}
}
const TAU = Math.PI * 2;

function pad(start, length, midi, level, pan) {
	const f = hz(midi);
	voice(dry, start, length + 1.2, pan, (t) => {
		const attack = Math.min(1, t / 0.5), release = t > length ? Math.max(0, 1 - (t - length) / 1.2) : 1;
		const shimmer = 1 + 0.12 * Math.sin(TAU * 0.31 * t + midi);
		return level * attack * release * shimmer * (Math.sin(TAU * f * 1.002 * t) + 0.35 * Math.sin(TAU * f * 2 * t) + 0.12 * Math.sin(TAU * f * 3.001 * t));
	});
	voice(wet, start, length + 1.2, -pan, (t) => {
		const attack = Math.min(1, t / 0.5), release = t > length ? Math.max(0, 1 - (t - length) / 1.2) : 1;
		return level * 0.5 * attack * release * Math.sin(TAU * f * 0.998 * t);
	});
}
function pluck(start, midi, level, pan) {
	const f = hz(midi);
	const sound = (t) => level * Math.min(1, t / 0.004) * (Math.sin(TAU * f * t) * Math.exp(-t * 5.5) + 0.3 * Math.sin(TAU * f * 2 * t) * Math.exp(-t * 11) + 0.14 * Math.sin(TAU * f * 5.4 * t) * Math.exp(-t * 32));
	voice(dry, start, 1.4, pan, sound);
	voice(echo, start, 1.4, pan, (t) => sound(t) * 0.6);
}
function bell(start, midi, level, pan) {
	const f = hz(midi);
	const sound = (t) => level * Math.min(1, t / 0.006) * (Math.sin(TAU * f * t) * Math.exp(-t * 1.4) + 0.45 * Math.sin(TAU * f * 2.76 * t) * Math.exp(-t * 4) + 0.2 * Math.sin(TAU * f * 5.4 * t) * Math.exp(-t * 9));
	voice(dry, start, 3.5, pan, sound);
	voice(wet, start, 3.5, pan, (t) => sound(t) * 0.8);
	voice(echo, start, 3.5, -pan, (t) => sound(t) * 0.4);
}
function lead(start, length, midi, level) {
	const f = hz(midi);
	let phase = 0;
	const sound = (t) => {
		const vibrato = t > 0.16 ? 0.0045 * Math.sin(TAU * 5.4 * t) : 0;
		phase += TAU * f * (1 + vibrato) / RATE;
		const attack = Math.min(1, t / 0.035), release = t > length ? Math.max(0, 1 - (t - length) / 0.16) : 1;
		return level * attack * release * (Math.sin(phase) + 0.18 * Math.sin(phase * 2) + 0.05 * Math.sin(phase * 3));
	};
	const samples = [];
	voice(dry, start, length + 0.16, 0.08, (t) => { const v = sound(t); samples.push(v); return v; });
	let i = 0;
	voice(echo, start, length + 0.16, -0.2, () => samples[i++] * 0.45);
	i = 0;
	voice(wet, start, length + 0.16, 0, () => samples[i++] * 0.5);
}
function bass(start, length, midi, level) {
	const f = hz(midi);
	voice(dry, start, length + 0.1, 0, (t) => {
		const release = t > length ? Math.max(0, 1 - (t - length) / 0.1) : 1;
		return level * Math.min(1, t / 0.012) * release * Math.exp(-t * 1.6) * Math.tanh(1.6 * (Math.sin(TAU * f * t) + 0.3 * Math.sin(TAU * f * 2 * t)));
	});
}
function kick(start, level) {
	let phase = 0;
	voice(dry, start, 0.4, 0, (t) => {
		phase += TAU * (42 + 75 * Math.exp(-t * 26)) / RATE;
		return level * Math.exp(-t * 8.5) * Math.sin(phase);
	});
}
function rim(start, level) {
	let low = 0;
	const sound = (t) => { const n = noise(); low += 0.25 * (n - low); return level * ((n - low) * Math.exp(-t * 26) + 0.5 * Math.sin(TAU * 196 * t) * Math.exp(-t * 34)); };
	voice(dry, start, 0.25, 0.12, sound);
	voice(wet, start, 0.25, 0.12, (t) => sound(t) * 0.35);
}
function hat(start, level, pan) {
	let low = 0;
	voice(dry, start, 0.09, pan, (t) => { const n = noise(); low += 0.6 * (n - low); return level * (n - low) * Math.exp(-t * 70); });
}

const CHORDS = [
	{ root: 41, notes: [53, 57, 60, 64] },
	{ root: 45, notes: [55, 57, 60, 64] },
	{ root: 38, notes: [53, 57, 60, 62] },
	{ root: 46, notes: [53, 58, 62, 65] },
];
const TENSE = { root: 38, notes: [50, 53, 57, 64] };
const FINAL = { root: 41, notes: [53, 57, 60, 64, 67] };
const MOTIF = [
	[[0, 1, 72], [1, 0.5, 74], [1.5, 1.5, 77], [3, 1, 76]],
	[[0, 1.5, 72], [1.5, 0.5, 69], [2, 2, 67]],
	[[0, 1, 74], [1, 0.5, 77], [1.5, 1.5, 81], [3, 1, 79]],
	[[0, 1, 77], [1, 1, 74], [2, 2, 72]],
];
const ARP = [0, 2, 1, 3, 2, 3, 1, 2];
const within = (bar, a, b) => bar >= a && bar <= b;
const LAST = BARS - 1;

for (let bar = 0; bar < BARS; bar++) {
	const at = (beat) => bar * BAR + beat * BEAT + (Math.round(beat * 2) % 2 === 1 && beat * 2 === Math.round(beat * 2) ? SWING : 0);
	const cycle = (((bar - 3) % 4) + 4) % 4;
	const chord = bar < 3 ? TENSE : bar === LAST ? FINAL : CHORDS[cycle];
	const night = within(bar, 48, 51);
	const actCard = [6, 7, 18, 19, 28, 29, 41, 42, 56, 57].includes(bar);
	const full = !actCard && (within(bar, 8, 27) || within(bar, 36, 47) || within(bar, 52, 66));
	const light = within(bar, 30, 35) || actCard;

	chord.notes.forEach((note, i) => pad(bar * BAR, bar === LAST ? BAR * 1.3 : BAR, note, bar < 3 ? 0.03 : night ? 0.05 : 0.04, (i - 1.5) * 0.35));

	if (bar < 3) {
		pluck(at(0), 74, 0.11, -0.3); pluck(at(1.5), 69, 0.08, 0.3); pluck(at(3), 76, 0.09, 0.1);
		bass(at(0), BAR * 0.9, 38, 0.16);
		if (bar === 2) [60, 64, 67, 72].forEach((note, i) => pluck(at(3 + i * 0.25), note, 0.1 + i * 0.02, i * 0.2 - 0.3));
		continue;
	}
	if (night) {
		const first = MOTIF[cycle][0];
		bell(at(0), first[2] + 12, 0.11, -0.25);
		bell(at(2), chord.notes[2] + 12, 0.08, 0.3);
		bell(at(3.5), chord.notes[1] + 24, 0.05, 0);
		bass(at(0), BAR * 0.9, chord.root, 0.14);
		continue;
	}
	if (bar === LAST) {
		kick(at(0), 0.5); bass(at(0), BAR, chord.root, 0.3);
		[65, 69, 72, 76, 79, 84].forEach((note, i) => pluck(at(i * 0.25), note, 0.14, (i - 2.5) * 0.25));
		bell(at(1.5), 89, 0.09, 0);
		continue;
	}

	ARP.forEach((index, step) => pluck(at(step * 0.5), chord.notes[index] + 12, (step % 2 ? 0.075 : 0.11) * (bar < 6 ? 0.9 : 1), step % 2 ? 0.45 : -0.45));
	if (bar >= 4) {
		bass(at(0), BEAT * 1.4, chord.root, 0.3);
		bass(at(1.5), BEAT * 0.45, chord.root, 0.2);
		bass(at(2.5), BEAT * 0.45, chord.root + 12, 0.16);
		bass(at(3), BEAT * 0.9, chord.root + 7, 0.22);
	}
	if (full) {
		kick(at(0), 0.5); kick(at(2), 0.42); if (bar % 2) kick(at(2.5), 0.3);
		rim(at(1), 0.2); rim(at(3), 0.22);
		for (let step = 0; step < 8; step++) hat(at(step * 0.5), step % 2 ? 0.075 : 0.04, step % 2 ? 0.35 : -0.2);
	}
	if (light) {
		kick(at(0), 0.36); rim(at(3), 0.12);
		for (let step = 1; step < 8; step += 2) hat(at(step * 0.5), 0.055, 0.3);
	}
	if ([5, 7, 19, 29, 42, 57].includes(bar)) for (let step = 0; step < 8; step++) rim(at(2 + step * 0.25), 0.05 + step * 0.022);
	if (!actCard && (within(bar, 12, 17) || within(bar, 23, 27) || within(bar, 36, 40) || within(bar, 44, 47) || within(bar, 52, 55) || within(bar, 60, 66))) MOTIF[cycle].forEach(([beat, length, note]) => lead(at(beat), length * BEAT * 0.92, note, 0.1));
	if (light) MOTIF[cycle].slice(0, 2).forEach(([beat, , note]) => bell(at(beat), note + 12, 0.06, 0.2));
}

const delay = Math.round(BEAT * 0.75 * RATE);
for (let i = delay; i < total; i++) {
	echo[0][i] += echo[1][i - delay] * 0.42;
	echo[1][i] += echo[0][i - delay] * 0.42;
}
function reverb(input, offset) {
	const result = new Float32Array(total);
	for (const size of [1557, 1617, 1491, 1422, 1277, 1356]) {
		const line = new Float32Array(size + offset);
		let index = 0, damp = 0;
		for (let i = 0; i < total; i++) {
			const delayed = line[index];
			damp = delayed * 0.72 + damp * 0.28;
			line[index] = input[i] + damp * 0.84;
			index = (index + 1) % line.length;
			result[i] += delayed;
		}
	}
	for (const size of [225, 556]) {
		const line = new Float32Array(size + offset);
		let index = 0;
		for (let i = 0; i < total; i++) {
			const delayed = line[index];
			const value = result[i];
			line[index] = value + delayed * 0.5;
			result[i] = delayed - value * 0.5;
			index = (index + 1) % line.length;
		}
	}
	return result;
}
const send = [0, 1].map((ch) => { const mixed = new Float32Array(total); for (let i = 0; i < total; i++) mixed[i] = wet[ch][i] + echo[ch][i] * 0.5; return mixed; });
const room = [reverb(send[0], 0), reverb(send[1], 23)];

const master = [new Float32Array(total), new Float32Array(total)];
let peak = 0;
const fadeFrom = (BARS * BAR - 1.2) * RATE;
for (let ch = 0; ch < 2; ch++) {
	let low = 0, crackle = 0;
	for (let i = 0; i < total; i++) {
		crackle = crackle * 0.92 + (Math.abs(noise()) > 0.9993 ? noise() * 0.05 : 0);
		const hiss = noise() * 0.0022 + crackle;
		const sum = dry[ch][i] + echo[ch][i] * 0.55 + room[ch][i] * 0.11 + hiss;
		low += 0.55 * (sum - low);
		const fade = i > fadeFrom ? Math.max(0, 1 - (i - fadeFrom) / (2.6 * RATE)) : 1;
		const value = Math.tanh(low * 1.5) * fade;
		master[ch][i] = value;
		peak = Math.max(peak, Math.abs(value));
	}
}

const pcm = Buffer.alloc(44 + total * 4);
pcm.write("RIFF", 0); pcm.writeUInt32LE(36 + total * 4, 4); pcm.write("WAVEfmt ", 8); pcm.writeUInt32LE(16, 16);
pcm.writeUInt16LE(1, 20); pcm.writeUInt16LE(2, 22); pcm.writeUInt32LE(RATE, 24); pcm.writeUInt32LE(RATE * 4, 28);
pcm.writeUInt16LE(4, 32); pcm.writeUInt16LE(16, 34); pcm.write("data", 36); pcm.writeUInt32LE(total * 4, 40);
const gain = 0.84 / peak;
for (let i = 0; i < total; i++) {
	pcm.writeInt16LE(Math.round(master[0][i] * gain * 32767), 44 + i * 4);
	pcm.writeInt16LE(Math.round(master[1][i] * gain * 32767), 46 + i * 4);
}
const wav = output.replace(/\.mp3$/, ".wav");
writeFileSync(wav, pcm);
execFileSync("ffmpeg", ["-y", "-loglevel", "error", "-i", wav, "-b:a", "128k", output]);
console.log(`wrote ${output}`);
