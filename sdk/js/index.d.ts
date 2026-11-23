export class CorgiError extends Error {
  status: number;
  path: string;
}

export interface Paired {
  url: string;
  token: string;
  device: string;
  machine?: string;
  version?: string;
}

export function pair(opts: { url: string; code: string; device: string; fetch?: typeof fetch }): Promise<Paired>;

export type Duration = string | number;

export interface Task {
  id: number;
  ref: string;
  title: string;
  body?: string;
  workspace?: string;
  state: string;
  owner?: string;
  priority?: string;
  tags?: string[];
  comments?: { by: string; text: string; at: string }[];
  leaseUntil: string;
}

export interface Message {
  id: string;
  room: string;
  thread?: string;
  by: string;
  text?: string;
  shape?: string;
  data?: unknown;
  labels?: Record<string, string>;
  at: string;
}

export interface RoomInfo {
  name: string;
  messages: number;
  last?: string;
  lastBy?: string;
}

export interface Client {
  call(path: string, opts?: { method?: string; body?: unknown; query?: Record<string, unknown> }): Promise<any>;
  tasks: {
    next(opts?: { tags?: string[]; workspace?: string; lease?: string; wait?: Duration }): Promise<Task | null>;
    renew(ref: string, opts?: { lease?: string }): Promise<{ ref: string; leaseUntil: string }>;
    finish(ref: string, result: string): Promise<{ ref: string; state: string }>;
    add(task: { title: string; body?: string; workspace?: string }): Promise<{ ref: string }>;
    keep(ref: string, opts?: { lease?: string; every?: Duration }): Promise<() => Promise<void>>;
  };
  rooms: {
    list(): Promise<RoomInfo[]>;
    read(room: string, opts?: { after?: string; thread?: string; labels?: Record<string, string>; wait?: Duration }): Promise<Message[]>;
    post(room: string, msg: { text?: string; thread?: string; shape?: string; data?: unknown; labels?: Record<string, string> }): Promise<Message>;
    follow(room: string, opts?: { after?: string; thread?: string; labels?: Record<string, string>; signal?: AbortSignal }): AsyncGenerator<Message>;
  };
}

export function connect(opts: { url: string; token: string; fetch?: typeof fetch }): Client;
