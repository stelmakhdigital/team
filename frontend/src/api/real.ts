import type { Api } from './types';
import type { GetAgentSpecResponse, ListAgentSpecsResponse, ListProjectFilesResponse } from '../types/api';
import { http, toQuery } from './http';
import { getApiConfig } from './config';
import { ApiClientError, parseErrorBody } from './errors';

const B = '/api/v1';

/** Real REST implementation — used when VITE_API_MODE=real. */
export function createRealAdapter(): Api {
  return {
    teams: {
      getTeams: (params) => http(`${B}/teams${toQuery({ state: params?.state })}`),
      createTeam: (req) => http(`${B}/teams`, { method: 'POST', body: req }),
      exportTeam: (id) => http(`${B}/teams/${id}/export`),
      importTeam: (bundle) => http(`${B}/teams/import`, { method: 'POST', body: bundle }),
      getTeam: (id) => http(`${B}/teams/${id}`),
      setTeamState: (id, state) => http(`${B}/teams/${id}`, { method: 'PATCH', body: { state } }),
      deleteTeam: (id, hard) => http(`${B}/teams/${id}${hard ? '?hard=true' : ''}`, { method: 'DELETE' }),
      getTopology: (id) => http(`${B}/teams/${id}/topology`),
      createSegment: (teamId, req) => http(`${B}/teams/${teamId}/segments`, { method: 'POST', body: req }),
      createRole: (segmentId, req) => http(`${B}/segments/${segmentId}/roles`, { method: 'POST', body: req }),
      createRelative: (teamId, req) => http(`${B}/teams/${teamId}/relatives`, { method: 'POST', body: req }),
      updateSegmentLayout: (id, req) => http(`${B}/segments/${id}/layout`, { method: 'PATCH', body: req }),
      updateSegment: (id, req) => http(`${B}/segments/${id}`, { method: 'PATCH', body: req }),
      updateRoleLayout: (id, req) => http(`${B}/roles/${id}/layout`, { method: 'PATCH', body: req }),
      deleteRelative: (id) => http(`${B}/relatives/${id}`, { method: 'DELETE' }),
      deleteRole: (id) => http(`${B}/roles/${id}`, { method: 'DELETE' }),
      deleteSegment: (id) => http(`${B}/segments/${id}`, { method: 'DELETE' }),
      getRoleConfig: (id) => http(`${B}/roles/${id}/config`),
      updateRoleConfig: (id, req) => http(`${B}/roles/${id}/config`, { method: 'PATCH', body: req }),
      validateTopology: (id, req) => http(`${B}/teams/${id}/validate`, { method: 'POST', body: req ?? {} }),
      saveTopology: (id, req) => http(`${B}/teams/${id}/save`, { method: 'POST', body: req ?? {} }),
      listAgentSpecs: () => http<ListAgentSpecsResponse>(`${B}/agentspecs`),
      getAgentSpec: (name) => http<GetAgentSpecResponse>(`${B}/agentspecs/${encodeURIComponent(name)}`),
    },
    workflows: {
      // NOTE: GET /workflows is not in the frozen contract yet (blockers #3)
      getWorkflows: (params) => http(`${B}/workflows${toQuery({ team_id: params?.team_id, state: params?.state })}`),
      getWorkflow: (id) => http(`${B}/workflows/${id}`),
      createWorkflow: (req) => http(`${B}/workflows`, { method: 'POST', body: req }),
      createBlock: (workflowId, req) => http(`${B}/workflows/${workflowId}/blocks`, { method: 'POST', body: req }),
      createConnection: (workflowId, req) => http(`${B}/workflows/${workflowId}/connections`, { method: 'POST', body: req }),
      updateBlock: (workflowId, blockId, req) => http(`${B}/workflows/${workflowId}/blocks/${blockId}`, { method: 'PATCH', body: req }),
      updateWorkflow: (id, req) => http(`${B}/workflows/${id}`, { method: 'PATCH', body: req }),
      // P1.4: engine
      runWorkflow: (id, req) => http(`${B}/workflows/${id}/run`, { method: 'POST', body: req ?? {} }),
      listWorkflowInstances: (id) => http(`${B}/workflows/${id}/instances`),
      getWorkflowInstance: (iid) => http(`${B}/workflow-instances/${iid}`),
      getWorkflowInstanceTrace: (iid) => http(`${B}/workflow-instances/${iid}/trace`),
      resumeWorkflowInstance: (iid) => http(`${B}/workflow-instances/${iid}/resume`, { method: 'POST', body: {} }),
      routeWorkflowInstance: (iid, toRoleId) => http(`${B}/workflow-instances/${iid}/route`, { method: 'POST', body: { to_role_id: toRoleId } }),
      abortWorkflowInstance: (iid) => http(`${B}/workflow-instances/${iid}/abort`, { method: 'POST', body: {} }),
      deleteBlock: (workflowId, blockId) => http(`${B}/workflows/${workflowId}/blocks/${blockId}`, { method: 'DELETE' }),
      deleteConnection: (workflowId, connectionId) => http(`${B}/workflows/${workflowId}/connections/${connectionId}`, { method: 'DELETE' }),
      deleteWorkflow: (id) => http(`${B}/workflows/${id}`, { method: 'DELETE' }),
    },
    dashboard: {
      getSummary: () => http(`${B}/dashboard/summary`),
      getTasks: () => http(`${B}/dashboard/tasks`),
      getSessions: () => http(`${B}/dashboard/sessions`),
      getAlerts: () => http(`${B}/dashboard/alerts`),
      getMetrics: (params) => http(`${B}/dashboard/metrics${toQuery({ range: params?.range })}`),
    },
    attention: {
      list: (params) => http(`${B}/attention${toQuery({ limit: params?.limit, offset: params?.offset })}`),
    },
    events: {
      list: (params) =>
        http(
          `${B}/events${toQuery({
            type: params?.type,
            team_id: params?.team_id,
            role_id: params?.role_id,
            entity_kind: params?.entity_kind,
            entity_id: params?.entity_id,
            q: params?.q, // W5.2
            limit: params?.limit,
            offset: params?.offset,
          })}`,
        ), // P2.9
    },
    sessions: {
      list: (params) => http(`${B}/sessions${toQuery({ team_id: params?.team_id, role_id: params?.role_id, state: params?.state })}`),
      search: (params) =>
        http(
          `${B}/sessions/search${toQuery({
            q: params.q,
            role_id: params.role_id,
            team_id: params.team_id,
            state: params.state,
            limit: params.limit,
            offset: params.offset,
          })}`,
        ), // P2.7
      create: (teamId, req) => http(`${B}/sessions?team_id=${teamId}`, { method: 'POST', body: req }),
      get: (id) => http(`${B}/sessions/${id}`),
      stop: (id) => http(`${B}/sessions/${id}`, { method: 'DELETE' }),
      capture: (id, lines) => http(`${B}/sessions/${id}/capture${toQuery({ lines })}`), // P2.8
      send: (id, text, submit) => http(`${B}/sessions/${id}/send`, { method: 'POST', body: { text, submit } }), // P2.11
      snapshot: (id, req) => http(`${B}/sessions/${id}/snapshot`, { method: 'POST', body: req }), // P3.12
      getCompactPlan: () => http(`${B}/compact-plan`), // P1.5
      handover: (id, req) => http(`${B}/sessions/${id}/handover`, { method: 'POST', body: req }), // P1.5
      previewBriefing: (teamId, req) => http(`${B}/teams/${teamId}/briefing/preview`, { method: 'POST', body: req }), // P1.7
    },
    tasks: {
      list: (params) =>
        http(
          `${B}/tasks${toQuery({
            team_id: params?.team_id,
            state: params?.state,
            destination_role_id: params?.destination_role_id,
            limit: params?.limit,
            offset: params?.offset,
          })}`,
        ),
      overdue: (params) => http(`${B}/tasks/overdue${toQuery({ limit: params?.limit, offset: params?.offset })}`),
      create: (req) => http(`${B}/tasks`, { method: 'POST', body: req }),
      get: (id) => http(`${B}/tasks/${id}`),
      updateState: (id, req) => http(`${B}/tasks/${id}/state`, { method: 'PATCH', body: req }),
      handoff: (id, req) => http(`${B}/tasks/${id}/handoff`, { method: 'POST', body: req }),
      resolve: (id, req) => http(`${B}/tasks/${id}/resolve`, { method: 'POST', body: req }),
      // P3.13: proof/evidence
      submitEvidence: (id, req) => http(`${B}/tasks/${id}/evidence`, { method: 'POST', body: req }),
      listEvidence: (id, params) => http(`${B}/tasks/${id}/evidence${toQuery({ limit: params?.limit, offset: params?.offset })}`),
      verdictEvidence: (id, evidenceId, req) => http(`${B}/tasks/${id}/evidence/${evidenceId}/verdict`, { method: 'POST', body: req }),
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
    // P3.15: mailbox (inbox/outbox + ack)
    mailbox: {
      getInbox: (roleId, params) =>
        http(`${B}/roles/${roleId}/inbox${toQuery({ limit: params?.limit, offset: params?.offset })}`),
      getOutbox: (roleId, params) =>
        http(`${B}/roles/${roleId}/outbox${toQuery({ limit: params?.limit, offset: params?.offset })}`),
      ack: (messageId, action) => http(`${B}/messages/${messageId}/ack`, { method: 'POST', body: { action } }),
    },
    // P3.14: Walk (paced-доставка контекста в сессию)
    walks: {
      list: (sessionId) => http(`${B}/sessions/${sessionId}/walks`),
      get: (id) => http(`${B}/walks/${id}`),
      create: (sessionId, req) => http(`${B}/sessions/${sessionId}/walks`, { method: 'POST', body: req }),
      advance: (id) => http(`${B}/walks/${id}/advance`, { method: 'POST' }),
      pause: (id) => http(`${B}/walks/${id}/pause`, { method: 'POST' }),
      resume: (id) => http(`${B}/walks/${id}/resume`, { method: 'POST' }),
      skip: (id) => http(`${B}/walks/${id}/skip`, { method: 'POST' }),
    },
    // P3.16: Mode/permission posture (delegated vs human-led)
    posture: {
      setTeam: (teamId, posture) =>
        http(`${B}/teams/${teamId}/posture`, { method: 'PATCH', body: { posture } }),
      setSession: (sessionId, posture) =>
        http(`${B}/sessions/${sessionId}/posture`, { method: 'PATCH', body: { posture } }),
      getHumanRequest: (id) => http(`${B}/human-requests/${id}`),
      decideHumanRequest: (id, decision, reason) =>
        http(`${B}/human-requests/${id}/decide`, { method: 'POST', body: { decision, reason: reason ?? null } }),
      listHumanRequests: () => http(`${B}/human-requests`),
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
      updateLibraryItem: (id, req) => http(`${B}/library/${id}`, { method: 'PATCH', body: req }),
      applyLibrary: (id, req) => http(`${B}/library/${id}/apply`, { method: 'POST', body: req }),
      pin: (id) => http(`${B}/library/${id}/pin`, { method: 'POST' }), // P3.12
      unpin: (id) => http(`${B}/library/${id}/unpin`, { method: 'POST' }), // P3.12
      delete: (id) => http(`${B}/library/${id}`, { method: 'DELETE' }), // P3.12: prune
      launch: (id, req) => http(`${B}/library/${id}/launch`, { method: 'POST', body: req }), // P3.12
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
    config: {
      getConfig: () => http(`${B}/config`),
      setConfig: (key, value) => http(`${B}/config/${encodeURIComponent(key)}`, { method: 'PUT', body: { value } }),
      resetConfig: (key) => http(`${B}/config/${encodeURIComponent(key)}`, { method: 'DELETE' }),
    },
    // P1.0: Projects
    projects: {
      list: (params) => http(`${B}/projects${toQuery({
        limit: params?.limit, offset: params?.offset, name: params?.name,
        archived: params?.archived ? 'true' : undefined,
        include_archived: params?.include_archived ? 'true' : undefined,
      })}`),
      create: (req) => http(`${B}/projects`, { method: 'POST', body: req }),
      get: (id) => http(`${B}/projects/${id}`),
      update: (id, req) => http(`${B}/projects/${id}`, { method: 'PATCH', body: req }),
      archive: (id) => http(`${B}/projects/${id}`, { method: 'DELETE' }),
      listFiles: (id, path, opts) => http<ListProjectFilesResponse>(`${B}/projects/${id}/files${toQuery({ path, attribute: opts?.attribute ? 'true' : undefined })}`),
      getFile: async (id, path) => {
        const cfg = getApiConfig();
        const headers: Record<string, string> = { Accept: 'text/plain, application/json' };
        if (cfg.apiKey) headers['X-API-Key'] = cfg.apiKey;
        const url = `${cfg.baseUrl}${B}/projects/${id}/files/${encodeURI(path).split('/').map(encodeURIComponent).join('/')}`;
        const res = await fetch(url, { headers });
        if (!res.ok) {
          const text = await res.text().catch(() => '');
          let parsed: unknown = null;
          try { parsed = JSON.parse(text); } catch { parsed = text; }
          const e = parseErrorBody(parsed, res.status);
          throw new ApiClientError(res.status, e.code, e.message, e.details);
        }
        const content = await res.text();
        const contentType = res.headers.get('Content-Type') ?? 'text/plain';
        const disposition = res.headers.get('Content-Disposition') ?? '';
        const m = disposition.match(/filename="?([^";]+)"?/);
        return { content, contentType, filename: m ? m[1] : path.split('/').pop() ?? path };
      },
    },
    watchdogJobs: {
      list: () => http(`${B}/watchdog/jobs`),
      get: (id) => http(`${B}/watchdog/jobs/${id}`),
      register: (req) => http(`${B}/watchdog/jobs`, { method: 'POST', body: req }),
      stop: (id) => http(`${B}/watchdog/jobs/${id}/stop`, { method: 'POST' }),
      remove: (id) => http(`${B}/watchdog/jobs/${id}`, { method: 'DELETE' }),
    },
    diagnostics: { // P1.8
      get: () => http(`${B}/diagnostics`),
      run: () => http(`${B}/diagnostics/run`, { method: 'POST' }),
    },
  };
}
