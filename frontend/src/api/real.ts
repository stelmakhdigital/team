import type { Api } from './types';
import { http, toQuery } from './http';

const B = '/api/v1';

/** Real REST implementation — used when VITE_API_MODE=real. */
export function createRealAdapter(): Api {
  return {
    teams: {
      getTeams: () => http(`${B}/teams`),
      createTeam: (req) => http(`${B}/teams`, { method: 'POST', body: req }),
      getTeam: (id) => http(`${B}/teams/${id}`),
      getTopology: (id) => http(`${B}/teams/${id}/topology`),
      createSegment: (teamId, req) => http(`${B}/teams/${teamId}/segments`, { method: 'POST', body: req }),
      createRole: (segmentId, req) => http(`${B}/segments/${segmentId}/roles`, { method: 'POST', body: req }),
      createRelative: (teamId, req) => http(`${B}/teams/${teamId}/relatives`, { method: 'POST', body: req }),
      updateSegmentLayout: (id, req) => http(`${B}/segments/${id}/layout`, { method: 'PATCH', body: req }),
      updateRoleLayout: (id, req) => http(`${B}/roles/${id}/layout`, { method: 'PATCH', body: req }),
      deleteRelative: (id) => http(`${B}/relatives/${id}`, { method: 'DELETE' }),
      getRoleConfig: (id) => http(`${B}/roles/${id}/config`),
      updateRoleConfig: (id, req) => http(`${B}/roles/${id}/config`, { method: 'PATCH', body: req }),
      validateTopology: (id, req) => http(`${B}/teams/${id}/validate`, { method: 'POST', body: req ?? {} }),
      saveTopology: (id, req) => http(`${B}/teams/${id}/save`, { method: 'POST', body: req ?? {} }),
    },
    workflows: {
      // NOTE: GET /workflows is not in the frozen contract yet (blockers #3)
      getWorkflows: (params) => http(`${B}/workflows${toQuery({ team_id: params?.team_id, state: params?.state })}`),
      getWorkflow: (id) => http(`${B}/workflows/${id}`),
      createWorkflow: (req) => http(`${B}/workflows`, { method: 'POST', body: req }),
      createBlock: (workflowId, req) => http(`${B}/workflows/${workflowId}/blocks`, { method: 'POST', body: req }),
      createConnection: (workflowId, req) => http(`${B}/workflows/${workflowId}/connections`, { method: 'POST', body: req }),
      updateBlock: (workflowId, blockId, req) => http(`${B}/workflows/${workflowId}/blocks/${blockId}`, { method: 'PATCH', body: req }),
    },
    dashboard: {
      getSummary: () => http(`${B}/dashboard/summary`),
      getTasks: () => http(`${B}/dashboard/tasks`),
      getSessions: () => http(`${B}/dashboard/sessions`),
      getAlerts: () => http(`${B}/dashboard/alerts`),
      getMetrics: () => http(`${B}/dashboard/metrics`),
    },
    sessions: {
      list: (params) => http(`${B}/sessions${toQuery({ team_id: params?.team_id, role_id: params?.role_id, state: params?.state })}`),
      create: (teamId, req) => http(`${B}/sessions?team_id=${teamId}`, { method: 'POST', body: req }),
      get: (id) => http(`${B}/sessions/${id}`),
      stop: (id) => http(`${B}/sessions/${id}`, { method: 'DELETE' }),
    },
    messages: {
      getMessages: (params) =>
        http(`${B}/messages${toQuery({
          team_id: params?.team_id,
          queue_task_id: params?.queue_task_id,
          from_role_id: params?.from_role_id,
          to_role_id: params?.to_role_id,
          type: params?.type,
          limit: params?.limit,
          offset: params?.offset,
        })}`),
      sendMessage: (req) => http(`${B}/messages`, { method: 'POST', body: req }),
      getChatrooms: () => http(`${B}/chatrooms`),
      getChatroomMessages: (id) => http(`${B}/chatrooms/${id}/messages`),
      sendChatroomMessage: (id, req) => http(`${B}/chatrooms/${id}/messages`, { method: 'POST', body: req }),
    },
    library: {
      getLibrary: (params) =>
        http(`${B}/library${toQuery({
          type: params?.type,
          group: params?.group,
          search: params?.search,
          limit: params?.limit,
          offset: params?.offset,
        })}`),
      getLibraryItem: (id) => http(`${B}/library/${id}`),
      saveToLibrary: (req) => http(`${B}/library`, { method: 'POST', body: req }),
    },
    history: {
      getTaskHistory: (id) => http(`${B}/tasks/${id}/history`),
      getSessionHistory: (id) => http(`${B}/sessions/${id}/history`),
      getAuditLog: (params) =>
        http(`${B}/audit${toQuery({
          user_id: params?.user_id,
          action: params?.action,
          resource: params?.resource,
          start_time: params?.start_time,
          end_time: params?.end_time,
          limit: params?.limit,
          offset: params?.offset,
        })}`),
      getTranscript: (id) => http(`${B}/sessions/${id}/transcript`),
    },
  };
}
