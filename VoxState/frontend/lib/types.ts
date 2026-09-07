// Types mirror the backend's JSON response shapes exactly (see
// backend/internal/api/*.go). Kept in one file, hand-written rather than
// generated, since the API surface is small and stable — see
// docs/ARCHITECTURE.md's M8 section for why no codegen step was added.

export interface MachineResponse {
  id: string;
  name: string;
  type?: string;
  location?: string;
  created_at: string;
}

export interface StateResponse {
  machine_id: string;
  version: number;
  status: string;
  attributes?: Record<string, string>;
  created_at: string;
  caused_by_event_id?: string;
}

export interface EventResponse {
  id: string;
  type: string;
  machine_id?: string;
  payload?: Record<string, unknown>;
  resulting_state_version?: number;
  created_at: string;
}

export interface ChangeStateResponse {
  state: StateResponse;
  changed: boolean;
  events?: EventResponse[];
}

export type TaskStatus = "PENDING" | "RUNNING" | "COMPLETED" | "CANCELLED";

export interface TaskResponse {
  id: string;
  machine_id: string;
  tool_name: string;
  bound_version: number;
  status: TaskStatus;
  created_at: string;
  started_at?: string;
  finished_at?: string;
}

export interface ToolResultResponse {
  id: string;
  task_id: string;
  machine_id: string;
  bound_version: number;
  payload?: Record<string, unknown>;
  created_at: string;
}

export type PolicyOutcome = "ACCEPTED" | "REJECTED";

export interface DecisionResponse {
  outcome: PolicyOutcome;
  result: ToolResultResponse;
  current_version: number;
  reason?: string;
  event?: EventResponse;
}

export type AgentOutcome = "ACCEPTED" | "REJECTED";

export interface AgentResultResponse {
  selected_tool: string;
  task_id: string;
  bound_version: number;
  current_version: number;
  outcome: AgentOutcome;
  payload?: Record<string, unknown>;
  rejection_reason?: string;
  replan_required?: boolean;
}

export interface VoiceSessionResponse {
  session_id: string;
  machine_id: string;
  livekit_room_id: string;
  livekit_url: string;
  livekit_token: string;
  user_id?: string;
  started_at: string;
  ended_at?: string;
}

export interface ActivityEntryResponse {
  message: string;
  time: string;
  attrs?: Record<string, unknown>;
}

export interface ErrorResponse {
  error: string;
}
