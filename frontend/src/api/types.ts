import type {
  CreateBlockRequest,
  CreateBlockResponse,
  CreateConnectionRequest,
  CreateConnectionResponse,
  CreateRelativeRequest,
  CreateRelativeResponse,
  CreateRoleRequest,
  CreateRoleResponse,
  CreateSegmentRequest,
  CreateSegmentResponse,
  CreateTaskRequest,
  CreateTaskResponse,
  CreateTeamRequest,
  CreateTeamResponse,
  CreateWorkflowRequest,
  CreateWorkflowResponse,
  GetAlertsResponse,
  GetAttentionParams,
  GetAttentionResponse,
  GetAuditLogParams,
  GetAuditLogResponse,
  GetChatroomMessagesResponse,
  GetChatroomsResponse,
  GetLibraryItemResponse,
  GetLibraryParams,
  GetLibraryResponse,
  GetMetricsParams,
  GetMetricsResponse,
  GetMessagesParams,
  GetMessagesResponse,
  GetRoleConfigResponse,
  GetSessionHistoryResponse,
  GetSessionsResponse,
  GetTaskHistoryResponse,
  GetTeamResponse,
  GetTeamsResponse,
  GetTopologyResponse,
  GetTranscriptResponse,
  GetTasksResponse,
  GetTaskResponse,
  GetOverdueParams,
  GetWorkflowResponse,
  GetWorkflowsResponse,
  ListSessionsParams,
  TranscriptSearchParams,
  TranscriptSearchResult,
  CaptureResult,
  EventPage,
  GetEventsParams,
  ListSessionsResponse,
  CreateSessionRequest,
  CreateSessionResponse,
  DiagnosticsResult,
  SessionDetail,
  StopSessionResponse,
  ListTasksParams,
  UpdateTaskStateRequest,
  HandoffTaskRequest,
  ResolveTaskRequest,
  ResolveTaskResponse,
  HandoffTaskResponse,
  Task,
  ApplyLibraryItemRequest,
  ApplyLibraryItemResponse,
  RenameRequest,
  RenameResponse,
  SaveToLibraryRequest,
  SaveToLibraryResponse,
  SaveTopologyRequest,
  SaveTopologyResponse,
  SendChatroomMessageRequest,
  SendChatroomMessageResponse,
  SendMessageRequest,
  SendMessageResponse,
  UpdateBlockRequest,
  UpdateBlockResponse,
  DeleteWorkflowBlockResponse,
  DeleteWorkflowConnectionResponse,
  DeleteWorkflowResponse,
  UpdateRoleConfigRequest,
  UpdateRoleConfigResponse,
  UpdateRoleLayoutRequest,
  UpdateRoleLayoutResponse,
  UpdateSegmentLayoutRequest,
  UpdateSegmentLayoutResponse,
  ValidateTopologyRequest,
  ValidateTopologyResponse,
  DashboardSummaryResponse,
  GetWorkflowsParams,
  ListConfigResponse,
  SetConfigResponse,
  ResetConfigResponse,
  // P1.0: Projects
  ProjectResponse,
  ListProjectsResponse,
  ListProjectsParams,
  CreateProjectRequest,
  CreateProjectResponse,
  GetProjectResponse,
  UpdateProjectRequest,
  WatchdogJob,
  WatchdogJobDetail,
  WatchdogJobListResponse,
  WatchdogJobRegisterRequest,
  // P1.4: Workflow engine
  WorkflowInstance,
  WorkflowInstanceDetail,
  WorkflowStep,
  ListWorkflowInstancesResponse,
  // P1.5: handover + compact-plan
  BriefingPreviewRequest,
  BriefingPreviewResponse,
  CompactPlanResponse,
  HandoverRequest,
  HandoverResult,
} from '../types/api';

export interface DeleteRelativeResponse {
  id: number;
  status: 'deleted';
  from_role_name: string;
  to_role_name: string;
}

/** Single API surface used by the UI. Implemented by MockAdapter and
 * RealAdapter; the UI never knows which one is active. */
export interface Api {
  teams: {
    getTeams(): Promise<GetTeamsResponse>;
    createTeam(req: CreateTeamRequest): Promise<CreateTeamResponse>;
    getTeam(id: number): Promise<GetTeamResponse>;
    getTopology(id: number): Promise<GetTopologyResponse>;
    createSegment(teamId: number, req: CreateSegmentRequest): Promise<CreateSegmentResponse>;
    createRole(segmentId: number, req: CreateRoleRequest): Promise<CreateRoleResponse>;
    createRelative(teamId: number, req: CreateRelativeRequest): Promise<CreateRelativeResponse>;
    updateSegmentLayout(id: number, req: UpdateSegmentLayoutRequest): Promise<UpdateSegmentLayoutResponse>;
    updateSegment(id: number, req: RenameRequest): Promise<RenameResponse>;
    updateRoleLayout(id: number, req: UpdateRoleLayoutRequest): Promise<UpdateRoleLayoutResponse>;
    deleteRelative(id: number): Promise<DeleteRelativeResponse>;
    getRoleConfig(id: number): Promise<GetRoleConfigResponse>;
    updateRoleConfig(id: number, req: UpdateRoleConfigRequest): Promise<UpdateRoleConfigResponse>;
    validateTopology(id: number, req?: ValidateTopologyRequest): Promise<ValidateTopologyResponse>;
    saveTopology(id: number, req?: SaveTopologyRequest): Promise<SaveTopologyResponse>;
  };
  workflows: {
    getWorkflows(params?: GetWorkflowsParams): Promise<GetWorkflowsResponse>;
    getWorkflow(id: number): Promise<GetWorkflowResponse>;
    createWorkflow(req: CreateWorkflowRequest): Promise<CreateWorkflowResponse>;
    createBlock(workflowId: number, req: CreateBlockRequest): Promise<CreateBlockResponse>;
    createConnection(workflowId: number, req: CreateConnectionRequest): Promise<CreateConnectionResponse>;
    updateBlock(workflowId: number, blockId: number, req: UpdateBlockRequest): Promise<UpdateBlockResponse>;
    updateWorkflow(id: number, req: RenameRequest): Promise<RenameResponse>;
    // P1.4: engine
    runWorkflow(id: number, req?: { root_objective?: string }): Promise<{ instance: WorkflowInstance }>;
    listWorkflowInstances(id: number): Promise<ListWorkflowInstancesResponse>;
    getWorkflowInstance(instanceId: number): Promise<WorkflowInstanceDetail>;
    getWorkflowInstanceTrace(instanceId: number): Promise<{ steps: WorkflowStep[]; total: number }>;
    resumeWorkflowInstance(instanceId: number): Promise<{ instance: WorkflowInstance }>;
    routeWorkflowInstance(instanceId: number, toRoleId: number): Promise<{ instance: WorkflowInstance }>;
    abortWorkflowInstance(instanceId: number): Promise<{ instance: WorkflowInstance }>;
    deleteBlock(workflowId: number, blockId: number): Promise<DeleteWorkflowBlockResponse>;
    deleteConnection(workflowId: number, connectionId: number): Promise<DeleteWorkflowConnectionResponse>;
    deleteWorkflow(id: number): Promise<DeleteWorkflowResponse>;
  };
  dashboard: {
    getSummary(): Promise<DashboardSummaryResponse>;
    getTasks(): Promise<GetTasksResponse>;
    getSessions(): Promise<GetSessionsResponse>;
    getAlerts(): Promise<GetAlertsResponse>;
    getMetrics(params?: GetMetricsParams): Promise<GetMetricsResponse>;
  };
  attention: {
    list(params?: GetAttentionParams): Promise<GetAttentionResponse>;
  };
  events: {
    list(params?: GetEventsParams): Promise<EventPage>; // P2.9
  };
  sessions: {
    list(params?: ListSessionsParams): Promise<ListSessionsResponse>;
    search(params: TranscriptSearchParams): Promise<TranscriptSearchResult>; // P2.7
    capture(id: number, lines?: number): Promise<CaptureResult>; // P2.8
    create(teamId: number, req: CreateSessionRequest): Promise<CreateSessionResponse>;
    get(id: number): Promise<SessionDetail>;
    stop(id: number): Promise<StopSessionResponse>;
    getCompactPlan(): Promise<CompactPlanResponse>; // P1.5
    handover(id: number, req: HandoverRequest): Promise<HandoverResult>; // P1.5
    previewBriefing(teamId: number, req: BriefingPreviewRequest): Promise<BriefingPreviewResponse>; // P1.7
  };
  tasks: {
    list(params?: ListTasksParams): Promise<GetTasksResponse>;
    overdue(params?: GetOverdueParams): Promise<GetTasksResponse>;
    create(req: CreateTaskRequest): Promise<CreateTaskResponse>;
    get(id: number): Promise<GetTaskResponse>;
    updateState(id: number, req: UpdateTaskStateRequest): Promise<Task>;
    handoff(id: number, req: HandoffTaskRequest): Promise<HandoffTaskResponse>;
    resolve(id: number, req: ResolveTaskRequest): Promise<ResolveTaskResponse>;
  };
  messages: {
    getMessages(params?: GetMessagesParams): Promise<GetMessagesResponse>;
    sendMessage(req: SendMessageRequest): Promise<SendMessageResponse>;
    getChatrooms(): Promise<GetChatroomsResponse>;
    getChatroomMessages(chatroomId: number): Promise<GetChatroomMessagesResponse>;
    sendChatroomMessage(chatroomId: number, req: SendChatroomMessageRequest): Promise<SendChatroomMessageResponse>;
  };
  library: {
    getLibrary(params?: GetLibraryParams): Promise<GetLibraryResponse>;
    getLibraryItem(id: number): Promise<GetLibraryItemResponse>;
    saveToLibrary(req: SaveToLibraryRequest): Promise<SaveToLibraryResponse>;
    updateLibraryItem(id: number, req: RenameRequest): Promise<RenameResponse>;
    applyLibrary(id: number, req: ApplyLibraryItemRequest): Promise<ApplyLibraryItemResponse>;
  };
  history: {
    getTaskHistory(taskId: number): Promise<GetTaskHistoryResponse>;
    getSessionHistory(sessionId: number): Promise<GetSessionHistoryResponse>;
    getAuditLog(params?: GetAuditLogParams): Promise<GetAuditLogResponse>;
    getTranscript(sessionId: number): Promise<GetTranscriptResponse>;
  };
  config: {
    getConfig(): Promise<ListConfigResponse>;
    setConfig(key: string, value: unknown): Promise<SetConfigResponse>;
    resetConfig(key: string): Promise<ResetConfigResponse>;
  };
  // P1.0: Projects (мультипроектность)
  projects: {
    list(params?: ListProjectsParams): Promise<ListProjectsResponse>;
    create(req: CreateProjectRequest): Promise<CreateProjectResponse>;
    get(id: number): Promise<GetProjectResponse>;
    update(id: number, req: UpdateProjectRequest): Promise<ProjectResponse>;
    archive(id: number): Promise<ProjectResponse>;
  };
  watchdogJobs: {
    list: () => Promise<WatchdogJobListResponse>;
    get: (id: number) => Promise<WatchdogJobDetail>;
    register: (req: WatchdogJobRegisterRequest) => Promise<WatchdogJob>;
    stop: (id: number) => Promise<WatchdogJob>;
    remove: (id: number) => Promise<void>;
  };
  diagnostics: { // P1.8: Health/Doctor-панель (read-only)
    get: () => Promise<DiagnosticsResult>;
    run: () => Promise<DiagnosticsResult>;
  };
}
