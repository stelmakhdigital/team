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
  GetWorkflowResponse,
  GetWorkflowsResponse,
  ListSessionsParams,
  ListSessionsResponse,
  CreateSessionRequest,
  CreateSessionResponse,
  SessionDetail,
  StopSessionResponse,
  ListTasksParams,
  UpdateTaskStateRequest,
  HandoffTaskRequest,
  HandoffTaskResponse,
  Task,
  ApplyLibraryItemRequest,
  ApplyLibraryItemResponse,
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
  sessions: {
    list(params?: ListSessionsParams): Promise<ListSessionsResponse>;
    create(teamId: number, req: CreateSessionRequest): Promise<CreateSessionResponse>;
    get(id: number): Promise<SessionDetail>;
    stop(id: number): Promise<StopSessionResponse>;
  };
  tasks: {
    list(params?: ListTasksParams): Promise<GetTasksResponse>;
    create(req: CreateTaskRequest): Promise<CreateTaskResponse>;
    get(id: number): Promise<GetTaskResponse>;
    updateState(id: number, req: UpdateTaskStateRequest): Promise<Task>;
    handoff(id: number, req: HandoffTaskRequest): Promise<HandoffTaskResponse>;
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
    applyLibrary(id: number, req: ApplyLibraryItemRequest): Promise<ApplyLibraryItemResponse>;
  };
  history: {
    getTaskHistory(taskId: number): Promise<GetTaskHistoryResponse>;
    getSessionHistory(sessionId: number): Promise<GetSessionHistoryResponse>;
    getAuditLog(params?: GetAuditLogParams): Promise<GetAuditLogResponse>;
    getTranscript(sessionId: number): Promise<GetTranscriptResponse>;
  };
}
