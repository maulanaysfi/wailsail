// Thin layer over the generated Wails bindings plus stream helpers.
import { Events } from "@wailsio/runtime";
import { Service as Kube } from "../../bindings/wailsail/internal/kube";
import { Service as Logs } from "../../bindings/wailsail/internal/logs";
import { Service as Exec } from "../../bindings/wailsail/internal/exec";
import { Service as PortForward } from "../../bindings/wailsail/internal/portforward";
import { Service as Settings } from "../../bindings/wailsail/internal/settings";

export type {
  ContextInfo,
  ResourceRef,
  ResourceType,
  ContainerInfo,
  ContainerPort,
  ApplyResult,
  ObjectDetail,
  EventInfo,
  ContainerMetrics,
} from "../../bindings/wailsail/internal/kube";
export type { Forward, Request as ForwardRequest } from "../../bindings/wailsail/internal/portforward";
export type { Settings as SettingsData } from "../../bindings/wailsail/internal/settings";
export type { Options as LogOptions } from "../../bindings/wailsail/internal/logs";

export { Kube, Logs, Exec, PortForward, Settings };

// Payload shapes of backend events (see internal/*/ Event types).
export interface Column {
  name: string;
  type: string;
  description: string;
  priority: number;
}
export interface OwnerRef {
  apiVersion: string;
  kind: string;
  name: string;
}
export interface PodResources {
  container?: string;
  cpuRequest?: string;
  cpuLimit?: string;
  memoryRequest?: string;
  memoryLimit?: string;
}
export interface Row {
  key: string;
  uid: string;
  name: string;
  namespace: string;
  created: string;
  cells: string[];
  owner?: OwnerRef;
  podResources?: PodResources;
}
export interface WatchEvent {
  reset?: boolean;
  columns?: Column[];
  upserts?: Row[];
  deletes?: string[];
  error?: string;
}
export interface LogEvent {
  connected?: boolean;
  lines?: string[];
  ended?: boolean;
  error?: string;
}
export interface ExecEvent {
  connected?: boolean;
  data?: string;
  exited?: boolean;
  error?: string;
}

export function newStreamId(): string {
  const a = new Uint8Array(8);
  crypto.getRandomValues(a);
  return Array.from(a, (b) => b.toString(16).padStart(2, "0")).join("");
}

/** Subscribe to a backend event; returns an unsubscribe function. */
export function on<T>(topic: string, handler: (data: T) => void): () => void {
  return Events.On(topic, (ev: { data: T }) => handler(ev.data));
}

export const topics = {
  watch: (id: string) => `wailsail:watch:${id}`,
  logs: (id: string) => `wailsail:logs:${id}`,
  exec: (id: string) => `wailsail:exec:${id}`,
  portForwards: "wailsail:portforwards",
};

/** Extract a human-readable message from a binding rejection. */
export function errorMessage(err: unknown): string {
  if (err == null) return "Unknown error";
  if (typeof err === "string") return err;
  if (err instanceof Error) return err.message;
  const anyErr = err as { message?: string };
  return anyErr.message ?? JSON.stringify(err);
}
