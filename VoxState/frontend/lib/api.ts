// Thin, typed fetch wrapper around the VoxState backend (see
// backend/internal/api/router.go for the exact route list this mirrors).
// This file contains no business logic and makes no policy/staleness
// decisions itself — every value it returns is exactly what the backend
// responded with; see docs/ARCHITECTURE.md's M8 section ("backend
// authority") for why that boundary matters.
import type {
  AgentResultResponse,
  ChangeStateResponse,
  DecisionResponse,
  ActivityEntryResponse,
  ErrorResponse,
  EventResponse,
  MachineResponse,
  StateResponse,
  TaskResponse,
  VoiceSessionResponse,
} from "./types";

export const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// ApiError carries the HTTP status alongside the backend's own error
// message (every error response is {"error": "..."}, see
// backend/internal/api/respond.go) so callers can distinguish, e.g., 404
// (not found) from 409 (conflict) without re-parsing the message text.
export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${API_BASE_URL}${path}`, {
      ...init,
      headers: {
        ...(init?.body ? { "Content-Type": "application/json" } : {}),
        ...init?.headers,
      },
    });
  } catch {
    throw new ApiError(
      0,
      `Could not reach the VoxState backend at ${API_BASE_URL}. Is it running?`,
    );
  }

  if (!res.ok) {
    let message = `Request failed with status ${res.status}`;
    try {
      const body = (await res.json()) as ErrorResponse;
      if (body?.error) message = body.error;
    } catch {
      // Non-JSON error body — fall back to the generic message above.
    }
    throw new ApiError(res.status, message);
  }

  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method: "POST",
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
}

export const api = {
  health: () => request<{ status: string; time: string }>("/health"),

  listMachines: () => request<MachineResponse[]>("/machines"),

  createMachine: (input: {
    id?: string;
    name: string;
    type?: string;
    location?: string;
    status?: string;
    attributes?: Record<string, string>;
  }) =>
    post<{ machine: MachineResponse; state: StateResponse; event: EventResponse }>(
      "/machines",
      input,
    ),

  getMachine: (id: string) =>
    request<MachineResponse>(`/machines/${encodeURIComponent(id)}`),

  getState: (id: string, version?: number) =>
    request<StateResponse>(
      `/machines/${encodeURIComponent(id)}/state${
        version !== undefined ? `?version=${version}` : ""
      }`,
    ),

  changeState: (
    id: string,
    input: {
      status?: string;
      attributes?: Record<string, string>;
      source?: string;
      note?: string;
    },
  ) =>
    post<ChangeStateResponse>(
      `/machines/${encodeURIComponent(id)}/state`,
      input,
    ),

  createTask: (machineId: string, toolName?: string) =>
    post<{ task: TaskResponse; event: EventResponse }>(
      `/machines/${encodeURIComponent(machineId)}/tasks`,
      toolName ? { tool_name: toolName } : undefined,
    ),

  listTasks: (machineId: string) =>
    request<TaskResponse[]>(
      `/machines/${encodeURIComponent(machineId)}/tasks`,
    ),

  getTask: (id: string) =>
    request<TaskResponse>(`/tasks/${encodeURIComponent(id)}`),

  startTask: (id: string, durationSeconds?: number) =>
    post<TaskResponse>(
      `/tasks/${encodeURIComponent(id)}/start`,
      durationSeconds ? { duration_seconds: durationSeconds } : undefined,
    ),

  cancelTask: (id: string) =>
    post<{ task: TaskResponse; event: EventResponse }>(
      `/tasks/${encodeURIComponent(id)}/cancel`,
    ),

  simulateResult: (taskId: string, payload?: Record<string, unknown>) =>
    post<DecisionResponse>(`/tasks/${encodeURIComponent(taskId)}/result`, {
      payload,
    }),

  runAgent: (machineId: string, instruction: string) =>
    post<AgentResultResponse>(
      `/machines/${encodeURIComponent(machineId)}/agent/run`,
      { instruction },
    ),

  startVoiceSession: (machineId: string, userId?: string) =>
    post<VoiceSessionResponse>(
      `/machines/${encodeURIComponent(machineId)}/voice/sessions`,
      userId ? { user_id: userId } : undefined,
    ),

  getVoiceSession: (id: string) =>
    request<VoiceSessionResponse>(
      `/voice/sessions/${encodeURIComponent(id)}`,
    ),

  endVoiceSession: (id: string) =>
    post<VoiceSessionResponse>(
      `/voice/sessions/${encodeURIComponent(id)}/end`,
    ),

  listActivity: () => request<ActivityEntryResponse[]>("/activity"),
};
