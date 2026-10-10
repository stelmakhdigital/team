import { Route, Routes } from 'react-router-dom';
import AppShell from './components/layout/AppShell';
import DashboardPage from './pages/DashboardPage';
import AttentionPage from './pages/AttentionPage';
import PulsePage from './pages/PulsePage';
import TeamsPage from './pages/TeamsPage';
import TeamBuilderPage from './pages/TeamBuilderPage';
import WorkflowsPage from './pages/WorkflowsPage';
import WorkflowEditorPage from './pages/WorkflowEditorPage';
import WorkflowInstancePage from './pages/WorkflowInstancePage';
import MessagesPage from './pages/MessagesPage';
import LibraryPage from './pages/LibraryPage';
import HistoryPage from './pages/HistoryPage';
import TasksPage from './pages/TasksPage';
import TaskDetailPage from './pages/TaskDetailPage';
import ProjectsPage from './pages/ProjectsPage';
import WatchdogJobsPage from './pages/WatchdogJobsPage';
import DiagnosticsPage from './pages/DiagnosticsPage';
import TranscriptSearchPage from './pages/TranscriptSearchPage';
import ProjectDetailPage from './pages/ProjectDetailPage';
import SessionDetailPage from './pages/SessionDetailPage';
import SettingsPage from './pages/SettingsPage';
import { EmptyState } from './components/ui/States';

function NotFound() {
  return (
    <div className="page">
      <EmptyState title="Page not found" hint="Check the URL or go back to the dashboard." />
    </div>
  );
}

export default function App() {
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/attention" element={<AttentionPage />} />
        <Route path="/pulse" element={<PulsePage />} />
        <Route path="/teams" element={<TeamsPage />} />
        <Route path="/teams/:teamId" element={<TeamBuilderPage />} />
        <Route path="/projects" element={<ProjectsPage />} />
        <Route path="/projects/:projectId" element={<ProjectDetailPage />} />
        <Route path="/tasks" element={<TasksPage />} />
        <Route path="/tasks/:taskId" element={<TaskDetailPage />} />
        <Route path="/sessions/:sessionId" element={<SessionDetailPage />} />
        <Route path="/workflows" element={<WorkflowsPage />} />
        <Route path="/workflows/:workflowId/instances/:instanceId" element={<WorkflowInstancePage />} />
        <Route path="/workflows/:workflowId" element={<WorkflowEditorPage />} />
        <Route path="/messages" element={<MessagesPage />} />
        <Route path="/library" element={<LibraryPage />} />
        <Route path="/watchdog/jobs" element={<WatchdogJobsPage />} />
        <Route path="/diagnostics" element={<DiagnosticsPage />} />
        <Route path="/transcript-search" element={<TranscriptSearchPage />} />
        <Route path="/history" element={<HistoryPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  );
}
