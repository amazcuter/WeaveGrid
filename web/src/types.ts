export interface Turning {
  ch: number;
  arc: number;
  kind: string;
}
export interface Thread {
  id: string;
  plant: number;
  payoff: number;
  reminds: number[];
  span: number;
  tag: string;
}
export interface Structure {
  chapters: number;
  seed: number;
  minGap: number;
  generatorVersion: string;
  arcs: [number, number][];
  nodes: Turning[];
  threads: Thread[];
  lines: [number, number][];
  fixedReminders: string[];
}
export interface Idea {
  id: string;
  chapter: number;
  text: string;
  required: boolean;
  active: boolean;
}
export interface Reference {
  id: string;
  title: string;
  text: string;
}
export interface Version {
  id: string;
  chapter: number;
  kind: string;
  text: string;
  summary: string;
  created: string;
  source: string;
  parent: string;
  summaryValid: boolean;
}
export interface ModelConfig {
  reasoningEffort?: string;
  baseUrl: string;
  model: string;
  credential: string;
  maxTokens: number;
  temperature: number;
  contextChars: number;
  timeoutSeconds: number;
  thinking: boolean;
}
export interface Settings {
  modelDefaultsVersion: number;
  orchestratorEnabled: boolean;
  orchestratorGuidance: string;
  models: Record<string, ModelConfig>;
  arcReviews: boolean;
  checkConcurrency: number;
  styleSamples: string;
  evalScenes: string;
  styleVerified: boolean;
  styleModelFingerprint: string;
  embeddingUrl: string;
  embeddingModel: string;
  embeddingCredential: string;
}
export interface Project {
  orchestratorMemory?: {
    summary: string;
    recent: {
      id: string;
      runId: string;
      chapter: number;
      gate: string;
      decision: string;
      created: string;
    }[];
    compactions: number;
    sequence: number;
    updated: string;
  };
  humanDecisions?: Proposal[];
  archived?: boolean;
  notes?: Note[];
  id: string;
  name: string;
  revision: number;
  created: string;
  structure: Structure;
  chapters: { number: number; outline: string; draft: string }[];
  versions: Version[];
  ideas: Idea[];
  references: Reference[];
  links: {
    id: string;
    from: number;
    to: number;
    reason: string;
    valid: boolean;
  }[];
  prompts: Record<string, PromptVersion[]>;
  settings: Settings;
  pendingEdits: {
    id: string;
    chapter: number;
    kind: string;
    text: string;
    base: string;
  }[];
  reviews: { id: string; mode: string; chapters: number[]; valid: boolean }[];
}
export interface PromptVersion {
  id: string;
  text: string;
  created: string;
}
export interface Citation {
  chapter: number;
  kind: string;
  version: string;
  paragraph: number;
  text: string;
  plan: boolean;
}
export interface Proposal {
  source?: string;
  id: string;
  chapter: number;
  kind: string;
  problem: string;
  instruction: string;
  evidence: Citation[];
  critical: boolean;
  decision: string;
  reason: string;
  applied: boolean;
}
export interface Change {
  chapter: number;
  kind: string;
  base: string;
  text: string;
  summary: string;
  reason: string;
}
export interface CreativeInput {
  source?: string;
  id: string;
  chapter: number;
  cycle: number;
  ideas: Idea[];
  emptyConfirmed: boolean;
  submittedAt: string;
}
export interface Run {
  orchestratorGate?: string;
  humanDecisions?: Proposal[];
  creativeInputs?: CreativeInput[];
  nextNode?: string;
  singleStep?: boolean;
  id: string;
  projectId: string;
  mode: string;
  status: string;
  stage: string;
  node: string;
  chapter: number;
  epoch: number;
  revision: number;
  cycle: number;
  created: string;
  updated: string;
  error: string;
  message: string;
  data: {
    creativeInput?: CreativeInput;
    contextIdeas?: Idea[];
    input?: { id: string; questions: string[]; answer: string };
    materials: Citation[];
    batch: Change[];
    queue: unknown[];
    proposals: Proposal[];
    reviewChapters: number[];
    reviewApproved: boolean;
  };
}
export interface Attempt {
  id: string;
  runId: string;
  node: string;
  chapter: number;
  status: string;
  prompt: string;
  promptVersion: string;
  modelConfig: ModelConfig;
  inputs: Citation[];
  output: string;
  reasoning: string;
  error: string;
  usage: { input: number; output: number; total: number };
  created: string;
  finished: string;
}
export interface FlowNode {
  id: string;
  name: string;
  description: string;
  steps: { id: string; name: string; ai: boolean; human?: boolean }[];
}
export interface Workflow {
  edges: Record<string, [string, string, string?][]>;
  actions: Record<string, string[]>;
  nodes: FlowNode[];
  defaults: Record<string, string>;
  variables: Record<string, boolean>;
}
export interface RunEvent {
  seq: number;
  runId: string;
  type: string;
  created: string;
  data: unknown;
}
export const labels: Record<string, string> = {
  running: "执行中",
  paused: "已暂停",
  waiting_input: "等待决定",
  failed: "待重试",
  stopped: "已终止",
  completed: "已完成",
  succeeded: "已完成",
  interrupted: "已中断",
  backward: "后向编纲",
  forward: "前向写作",
  outline: "大纲",
  draft: "正文",
  accepted: "已采用",
  rejected: "已拒绝",
  pending: "待决定",
};
export function current(p: Project, ch: number, kind: string) {
  const c = p.chapters[ch - 1];
  const id = kind === "draft" ? c?.draft : c?.outline;
  return p.versions.find((v) => v.id === id);
}

export interface Note {
  id: string;
  kind: string;
  title: string;
  text: string;
  chapter: number;
  created: string;
  updated: string;
}
export interface Checkpoint {
  id: string;
  projectId: string;
  name: string;
  note: string;
  created: string;
  projectRevision: number;
  mode: string;
  node: string;
  chapter: number;
  automatic?: boolean;
}
