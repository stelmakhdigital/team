
# Визуальный редактор топологий(Team Builder).

## Team Builder: Визуальный редактор топологий

### Что нужно реализовать

```
┌──────────────────────────────────────────────────────────┐
│                  TEAM BUILDER UI                         │
├──────────────────────────────────────────────────────────┤
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  TOOLBAR (слева)                                   │ │
│  │                                                    │ │
│  │  📦 Segments                                      │ │
│  │     - Backend                                     │ │
│  │     - Frontend                                    │ │
│  │     - Review                                      │ │
│  │     - Infra                                       │ │
│  │     - Custom...                                   │ │
│  │                                                    │ │
│  │  👤 Roles                                         │ │
│  │     - Lead                                        │ │
│  │     - Worker                                      │ │
│  │     - Reviewer                                    │ │
│  │     - Watchdog                                    │ │
│  │     - Custom...                                   │ │
│  │                                                    │ │
│  │  🔗 Relations                                     │ │
│  │     - delegates_to                                │ │
│  │     - spawned_by                                  │ │
│  │     - can_observe                                 │ │
│  │     - collaborates_with                           │ │
│  │                                                    │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  CANVAS (центр) - Drag & Drop                      │ │
│  │                                                    │ │
│  │     ┌─────────────┐                               │ │
│  │     │  Segment    │                               │ │
│  │     │  Backend    │                               │ │
│  │     │  ┌───────┐  │                               │ │
│  │     │  │ Lead  │──┼── delegates_to ────────┐     │ │
│  │     │  └───────┘  │                        │     │ │
│  │     │  ┌───────┐  │  ┌──────────────┐     │     │ │
│  │     │  │Worker │──┼──│   Segment    │     │     │ │
│  │     │  └───────┘  │  │   Review     │     │     │ │
│  │     │  ┌───────┐  │  │  ┌────────┐  │     │     │ │
│  │     │  │Worker │  │  │  │Reviewer│◄─┘     │     │ │
│  │     │  └───────┘  │  │  └────────┘  │     │     │ │
│  │     └─────────────┘  └──────────────┘     │     │ │
│  │                                            │     │ │
│  │  [Zoom: 100%] [Grid: On] [Snap: On]       │     │ │
│  └────────────────────────────────────────────┘     │ │
│                                                     │ │
│  ┌────────────────────────────────────────────────┐ │ │
│  │  CONFIG PANEL (справа)                         │ │ │
│  │                                                │ │ │
│  │  Selected: Backend.Lead                        │ │ │
│  │  ────────────────────────────────────────────  │ │ │
│  │                                                │ │ │
│  │  Name: [Lead            ]                      │ │ │
│  │  Agent: [pi-go-backend ▼]                      │ │ │
│  │  Profile: [project-x    ▼]                     │ │ │
│  │                                                │ │ │
│  │  Pi Config:                                    │ │ │
│  │  ┌──────────────────────────────────────────┐ │ │ │
│  │  │ ☑ git        ☑ github    ☑ terminal    │ │ │ │
│  │  │ ☑ filesystem ☐ http      ☐ MCP         │ │ │ │
│  │  │                                            │ │ │ │
│  │  │ Model: [claude-3-7-sonnet ▼]             │ │ │ │
│  │  │ Temperature: [0.7    ]                    │ │ │ │
│  │  │                                            │ │ │ │
│  │  │ Skills:                                   │ │ │ │
│  │  │  ☑ code-review                            │ │ │ │
│  │  │  ☑ tdd                                    │ │ │ │
│  │  │  ☑ go-style                               │ │ │ │
│  │  │  ☐ project-x-specific                     │ │ │ │
│  │  └──────────────────────────────────────────┘ │ │ │
│  │                                                │ │ │
│  │  Startup Files:                                │ │ │
│  │  ┌──────────────────────────────────────────┐ │ │ │
│  │  │ guidance/role.md                         │ │ │ │
│  │  │ guidance/context.md                      │ │ │ │
│  │  │ hooks/pre-commit.sh                      │ │ │ │
│  │  └──────────────────────────────────────────┘ │ │ │
│  │                                                │ │ │
│  │  [Save] [Cancel] [Delete]                      │ │ │
│  └────────────────────────────────────────────────┘ │ │
│                                                      │ │
│  ┌────────────────────────────────────────────────┐ │ │
│  │  BOTTOM PANEL                                  │ │ │
│  │                                                │ │ │
│  │  Validation:                                   │ │ │
│  │  ✅ All roles have agent_spec                 │ │ │
│  │  ⚠️  Role 'Worker2' has no outgoing edges    │ │ │
│  │  ✅ No circular dependencies                  │ │ │
│  │                                                │ │ │
│  │  [Validate] [Save to Library] [Deploy]        │ │ │
│  └────────────────────────────────────────────────┘ │ │
│                                                      │ │
└──────────────────────────────────────────────────────┘
```


______________________________________________________________________

## Детальные API Contracts для Team Builder

### 1. **Получить пустой холст**

```typescript
// GET /api/v1/teams/:id/topology
interface GetTopologyResponse {
  team: Team;
  segments: Segment[];
  roles: Role[];
  relatives: Relative[];
  
  // Для визуализации
  layout?: {
    segments: SegmentLayout[];
    roles: RoleLayout[];
    relatives: RelativeLayout[];
  };
}

interface SegmentLayout {
  segment_id: number;
  position: { x: number; y: number; width: number; height: number };
  collapsed: boolean;
}

interface RoleLayout {
  role_id: number;
  segment_id: number;
  position: { x: number; y: number };
}

interface RelativeLayout {
  relative_id: number;
  from_role_id: number;
  to_role_id: number;
  path?: Array<{ x: number; y: number }>;  // control points для кривой
}
```


### 2. **Создать сегмент на холсте**

```typescript
// POST /api/v1/teams/:teamId/segments
interface CreateSegmentRequest {
  name: string;
  description?: string;
  config?: Record<string, any>;
  
  // Для UI
  layout?: {
    x: number;
    y: number;
    width?: number;
    height?: number;
  };
}

interface CreateSegmentResponse {
  id: number;
  team_id: number;
  name: string;
  layout: {
    x: number;
    y: number;
    width: number;
    height: number;
  };
  status: 'created';
}
```


### 3. **Создать роль внутри сегмента**

```typescript
// POST /api/v1/segments/:segmentId/roles
interface CreateRoleRequest {
  name: string;
  agent_spec: string;
  profile?: string;
  config?: Record<string, any>;
  
  // Для UI
  layout?: {
    x: number;
    y: number;
  };
}

interface CreateRoleResponse {
  id: number;
  segment_id: number;
  name: string;
  address: string;
  layout: {
    x: number;
    y: number;
  };
  status: 'created';
}
```


### 4. **Создать связь (Relative)**

```typescript
// POST /api/v1/teams/:teamId/relatives
interface CreateRelativeRequest {
  from_role_id: number;
  to_role_id: number;
  type: 'delegates_to' | 'spawned_by' | 'can_observe' | 'collaborates_with';
  config?: Record<string, any>;
  
  // Для UI
  layout?: {
    path?: Array<{ x: number; y: number }>;  // control points
    label_position?: { x: number; y: number };
  };
}

interface CreateRelativeResponse {
  id: number;
  from_role_name: string;
  to_role_name: string;
  type: string;
  status: 'created';
}
```


### 5. **Обновить позицию (drag \& drop)**

```typescript
// PATCH /api/v1/segments/:id/layout
interface UpdateSegmentLayoutRequest {
  position: { x: number; y: number };
  size?: { width: number; height: number };
  collapsed?: boolean;
}

interface UpdateSegmentLayoutResponse {
  id: number;
  status: 'updated';
  previous_layout: SegmentLayout;
  new_layout: SegmentLayout;
}

// PATCH /api/v1/roles/:id/layout
interface UpdateRoleLayoutRequest {
  position: { x: number; y: number };
}

interface UpdateRoleLayoutResponse {
  id: number;
  status: 'updated';
  previous_position: { x: number; y: number };
  new_position: { x: number; y: number };
}
```


### 6. **Обновить связь (перетянуть линию)**

```typescript
// PATCH /api/v1/relatives/:id/layout
interface UpdateRelativeLayoutRequest {
  path?: Array<{ x: number; y: number }>;
  label_position?: { x: number; y: number };
}

interface UpdateRelativeLayoutResponse {
  id: number;
  status: 'updated';
}
```


### 7. **Удалить связь**

```typescript
// DELETE /api/v1/relatives/:id
interface DeleteRelativeResponse {
  id: number;
  status: 'deleted';
  from_role_name: string;
  to_role_name: string;
}
```


### 8. **Валидация топологии**

```typescript
// POST /api/v1/teams/:id/validate
interface ValidateTopologyRequest {
  check_circular?: boolean;
  check_orphans?: boolean;
  check_required_fields?: boolean;
}

interface ValidateTopologyResponse {
  is_valid: boolean;
  errors: TopologyError[];
  warnings: TopologyWarning[];
}

interface TopologyError {
  code: string;
  message: string;
  severity: 'error';
  location: {
    type: 'role' | 'segment' | 'relative';
    id: number;
    name: string;
  };
  fix_suggestion?: string;
}

interface TopologyWarning {
  code: string;
  message: string;
  severity: 'warning';
  location?: {
    type: 'role' | 'segment';
    id: number;
    name: string;
  };
  fix_suggestion?: string;
}

// Примеры ошибок:
// - ROLE_NO_AGENT_SPEC: "Role 'Worker' has no agent_spec"
// - CIRCULAR_DEPENDENCY: "Circular dependency detected: Lead → Worker → Lead"
// - ORPHAN_ROLE: "Role 'Reviewer2' has no incoming or outgoing edges"

// Примеры предупреждений:
// - NO_OUTGOING_EDGES: "Role 'Worker2' has no outgoing edges"
// - NO_INCOMING_EDGES: "Role 'Lead2' has no incoming edges"
// - DUPLICATE_ROLE_NAME: "Duplicate role name 'Lead' in segment 'Backend'"
```


### 9. **Сохранить топологию**

```typescript
// POST /api/v1/teams/:id/save
interface SaveTopologyRequest {
  name?: string;
  description?: string;
  save_to_library?: boolean;
  library_group?: string;
  is_public?: boolean;
}

interface SaveTopologyResponse {
  team_id: number;
  status: 'saved';
  library_item_id?: number;
  validation: ValidateTopologyResponse;
}
```


### 10. **Детальная конфигурация роли**

```typescript
// GET /api/v1/roles/:id/config
interface GetRoleConfigResponse {
  role: Role;
  agent_spec: AgentSpec;
  available_profiles: Profile[];
  available_skills: Skill[];
  available_plugins: Plugin[];
}

interface AgentSpec {
  name: string;
  version: string;
  runtime: {
    type: 'pi' | 'claude-code' | 'codex';
    version: string;
  };
  pi_config: {
    model: string;
    temperature: number;
    max_tokens: number;
    plugins: PluginSpec[];
    skills: SkillSpec[];
    hooks: HookSpec[];
    mcp: MCPConfig;
  };
  resources: {
    cpu: string;
    memory: string;
    gpu: boolean;
  };
  startup: {
    files: StartupFile[];
    scripts: StartupScript[];
  };
}

interface PluginSpec {
  name: string;
  enabled: boolean;
  config: Record<string, any>;
  description: string;
}

interface SkillSpec {
  path: string;
  enabled: boolean;
  description: string;
}

interface StartupFile {
  path: string;
  orientation: 'role' | 'context' | 'culture';
  delivery_hint: 'send_text' | 'project_file';
}

// PATCH /api/v1/roles/:id/config
interface UpdateRoleConfigRequest {
  agent_spec?: string;
  profile?: string;
  pi_config?: {
    model?: string;
    temperature?: number;
    plugins?: Record<string, boolean>;
    skills?: string[];
  };
  resources?: {
    cpu?: string;
    memory?: string;
  };
  startup?: {
    files?: StartupFile[];
  };
}

interface UpdateRoleConfigResponse {
  id: number;
  status: 'updated';
  previous_config: AgentSpec;
  new_config: AgentSpec;
  requires_restart: boolean;  // если нужно перезапустить сессию
}
```


### 11. **Предпросмотр конфигурации**

```typescript
// POST /api/v1/roles/preview
interface PreviewRoleConfigRequest {
  agent_spec: string;
  profile?: string;
  pi_config?: Record<string, any>;
}

interface PreviewRoleConfigResponse {
  generated_config: {
    pi_config_path: string;
    environment: Record<string, string>;
    command: string;
    plugins_enabled: string[];
    skills_enabled: string[];
  };
  validation: {
    is_valid: boolean;
    errors: string[];
    warnings: string[];
  };
  estimated_resources: {
    cpu: string;
    memory: string;
    gpu: boolean;
  };
}
```


### 12. **Импорт/Экспорт топологии**

```typescript
// POST /api/v1/teams/:id/export
interface ExportTopologyRequest {
  format: 'yaml' | 'json';
  include_layout?: boolean;
  include_history?: boolean;
}

interface ExportTopologyResponse {
  format: string;
  content: string;  // YAML/JSON строка
  download_url?: string;
}

// POST /api/v1/teams/import
interface ImportTopologyRequest {
  content: string;  // YAML/JSON
  format: 'yaml' | 'json';
  target_team_id?: number;  // если обновляем существующую
  merge_strategy?: 'replace' | 'merge';
}

interface ImportTopologyResponse {
  team_id: number;
  status: 'imported' | 'merged';
  created_resources: {
    segments: number;
    roles: number;
    relatives: number;
  };
  validation: ValidateTopologyResponse;
}
```


### 13. **История изменений топологии**

```typescript
// GET /api/v1/teams/:id/topology-history
interface GetTopologyHistoryResponse {
  versions: TopologyVersion[];
  total: number;
}

interface TopologyVersion {
  version_id: number;
  team_id: number;
  changed_by: string;  // user/role name
  changed_at: string;
  change_type: 'create' | 'update' | 'delete';
  changes: {
    segments_added?: number;
    segments_removed?: number;
    roles_added?: number;
    roles_removed?: number;
    relatives_added?: number;
    relatives_removed?: number;
  };
  diff_url?: string;  // ссылка на детальный diff
  can_restore: boolean;
}

// POST /api/v1/teams/:id/topology-history/:versionId/restore
interface RestoreTopologyVersionResponse {
  team_id: number;
  status: 'restored';
  restored_version: number;
  current_version: number;
}
```


______________________________________________________________________

## Frontend: React компоненты

### 1. **TeamCanvas (основной холст)**

```typescript
// components/TeamBuilder/TeamCanvas.tsx
interface TeamCanvasProps {
  teamId: number;
  onLayoutChange: (layout: TopologyLayout) => void;
  onSelectionChange: (selected: Selection) => void;
}

interface TopologyLayout {
  segments: SegmentLayout[];
  roles: RoleLayout[];
  relatives: RelativeLayout[];
}

interface Selection {
  type: 'segment' | 'role' | 'relative';
  id: number;
}

const TeamCanvas: React.FC<TeamCanvasProps> = ({ teamId, onLayoutChange, onSelectionChange }) => {
  const [topology, setTopology] = useState<TopologyLayout | null>(null);
  const [zoom, setZoom] = useState(1);
  const [grid, setGrid] = useState(true);
  const [snap, setSnap] = useState(true);
  
  // Загрузить топологию
  useEffect(() => {
    api.getTopology(teamId).then(setTopology);
  }, [teamId]);
  
  // Drag & Drop handlers
  const handleSegmentDrop = (segment: SegmentTemplate, position: Position) => {
    api.createSegment(teamId, {
      name: segment.name,
      layout: { x: position.x, y: position.y },
    }).then(newSegment => {
      setTopology(prev => ({
        ...prev!,
        segments: [...prev!.segments, { ...newSegment, layout: newSegment.layout }],
      }));
    });
  };
  
  const handleRoleDrop = (role: RoleTemplate, segmentId: number, position: Position) => {
    api.createRole(segmentId, {
      name: role.name,
      agent_spec: role.agent_spec,
      layout: { x: position.x, y: position.y },
    }).then(newRole => {
      setTopology(prev => ({
        ...prev!,
        roles: [...prev!.roles, { ...newRole, layout: newRole.layout }],
      }));
    });
  };
  
  const handleDragEnd = (type: 'segment' | 'role', id: number, newPosition: Position) => {
    if (type === 'segment') {
      api.updateSegmentLayout(id, { position: newPosition });
    } else {
      api.updateRoleLayout(id, { position: newPosition });
    }
  };
  
  const handleConnect = (fromRoleId: number, toRoleId: number, type: RelativeType) => {
    api.createRelative(teamId, {
      from_role_id: fromRoleId,
      to_role_id: toRoleId,
      type,
    }).then(newRelative => {
      setTopology(prev => ({
        ...prev!,
        relatives: [...prev!.relatives, newRelative],
      }));
    });
  };
  
  return (
    <div className="team-canvas">
      <Toolbar onSegmentDrop={handleSegmentDrop} onRoleDrop={handleRoleDrop} />
      
      <Canvas
        topology={topology}
        zoom={zoom}
        grid={grid}
        snap={snap}
        onSegmentDrop={handleSegmentDrop}
        onRoleDrop={handleRoleDrop}
        onDragEnd={handleDragEnd}
        onConnect={handleConnect}
        onSelectionChange={onSelectionChange}
      />
      
      <BottomBar
        zoom={zoom}
        grid={grid}
        snap={snap}
        onZoomChange={setZoom}
        onGridChange={setGrid}
        onSnapChange={setSnap}
        onValidate={() => api.validateTopology(teamId)}
        onSave={() => api.saveTopology(teamId)}
      />
    </div>
  );
};
```


### 2. **ConfigPanel (панель конфигурации)**

```typescript
// components/TeamBuilder/ConfigPanel.tsx
interface ConfigPanelProps {
  selection: Selection | null;
  onClose: () => void;
  onChange: () => void;  // trigger reload topology
}

const ConfigPanel: React.FC<ConfigPanelProps> = ({ selection, onClose, onChange }) => {
  const [config, setConfig] = useState<RoleConfig | null>(null);
  const [loading, setLoading] = useState(false);
  
  useEffect(() => {
    if (selection?.type === 'role' && selection.id) {
      setLoading(true);
      api.getRoleConfig(selection.id)
        .then(setConfig)
        .finally(() => setLoading(false));
    }
  }, [selection]);
  
  const handleSave = async () => {
    if (!config) return;
    
    await api.updateRoleConfig(selection!.id, config);
    onChange();  // reload topology
  };
  
  if (!selection) {
    return <EmptyState message="Select a role to configure" />;
  }
  
  return (
    <div className="config-panel">
      <PanelHeader
        title={`Configure ${selection.type} #${selection.id}`}
        onClose={onClose}
      />
      
      {loading ? (
        <LoadingSpinner />
      ) : config ? (
        <form onSubmit={handleSave}>
          {/* Basic Info */}
          <Section title="Basic Info">
            <TextField
              label="Name"
              value={config.role.name}
              onChange={name => setConfig({ ...config, role: { ...config.role, name } })}
            />
            
            <SelectField
              label="Agent Spec"
              value={config.agent_spec.name}
              options={availableAgentSpecs}
              onChange={agent_spec => setConfig({ ...config, agent_spec: { ...config.agent_spec, name: agent_spec } })}
            />
            
            <SelectField
              label="Profile"
              value={config.role.profile}
              options={config.available_profiles}
              onChange={profile => setConfig({ ...config, role: { ...config.role, profile } })}
            />
          </Section>
          
          {/* Pi Config */}
          <Section title="Pi Configuration">
            <SelectField
              label="Model"
              value={config.agent_spec.pi_config.model}
              options={AVAILABLE_MODELS}
              onChange={model => setConfig({
                ...config,
                agent_spec: {
                  ...config.agent_spec,
                  pi_config: { ...config.agent_spec.pi_config, model },
                },
              })}
            />
            
            <SliderField
              label="Temperature"
              value={config.agent_spec.pi_config.temperature}
              min={0}
              max={2}
              step={0.1}
              onChange={temperature => setConfig({
                ...config,
                agent_spec: {
                  ...config.agent_spec,
                  pi_config: { ...config.agent_spec.pi_config, temperature },
                },
              })}
            />
            
            <PluginGrid
              plugins={config.agent_spec.pi_config.plugins}
              onToggle={(pluginName, enabled) => setConfig({
                ...config,
                agent_spec: {
                  ...config.agent_spec,
                  pi_config: {
                    ...config.agent_spec.pi_config,
                    plugins: config.agent_spec.pi_config.plugins.map(p =>
                      p.name === pluginName ? { ...p, enabled } : p
                    ),
                  },
                },
              })}
            />
            
            <SkillList
              skills={config.agent_spec.pi_config.skills}
              onToggle={(skillPath, enabled) => setConfig({
                ...config,
                agent_spec: {
                  ...config.agent_spec,
                  pi_config: {
                    ...config.agent_spec.pi_config,
                    skills: config.agent_spec.pi_config.skills.map(s =>
                      s.path === skillPath ? { ...s, enabled } : s
                    ),
                  },
                },
              })}
            />
          </Section>
          
          {/* Resources */}
          <Section title="Resources">
            <SelectField
              label="CPU"
              value={config.agent_spec.resources.cpu}
              options={['0.5', '1.0', '2.0']}
              onChange={cpu => setConfig({
                ...config,
                agent_spec: {
                  ...config.agent_spec,
                  resources: { ...config.agent_spec.resources, cpu },
                },
              })}
            />
            
            <SelectField
              label="Memory"
              value={config.agent_spec.resources.memory}
              options={['256M', '512M', '1G', '2G']}
              onChange={memory => setConfig({
                ...config,
                agent_spec: {
                  ...config.agent_spec,
                  resources: { ...config.agent_spec.resources, memory },
                },
              })}
            />
            
            <CheckboxField
              label="GPU"
              checked={config.agent_spec.resources.gpu}
              onChange={gpu => setConfig({
                ...config,
                agent_spec: {
                  ...config.agent_spec,
                  resources: { ...config.agent_spec.resources, gpu },
                },
              })}
            />
          </Section>
          
          <PanelFooter>
            <Button type="button" variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" variant="primary">
              Save Configuration
            </Button>
          </PanelFooter>
        </form>
      ) : (
        <ErrorMessage message="Failed to load configuration" />
      )}
    </div>
  );
};
```


### 3. **ConnectionTool (инструмент для создания связей)**

```typescript
// components/TeamBuilder/ConnectionTool.tsx
interface ConnectionToolProps {
  fromRoleId: number | null;
  onConnect: (fromRoleId: number, toRoleId: number, type: RelativeType) => void;
  onCancel: () => void;
}

const ConnectionTool: React.FC<ConnectionToolProps> = ({ fromRoleId, onConnect, onCancel }) => {
  const [toRoleId, setToRoleId] = useState<number | null>(null);
  const [type, setType] = useState<RelativeType>('delegates_to');
  
  const handleRoleClick = (roleId: number) => {
    if (fromRoleId && roleId !== fromRoleId) {
      setToRoleId(roleId);
    }
  };
  
  const handleConfirm = () => {
    if (fromRoleId && toRoleId) {
      onConnect(fromRoleId, toRoleId, type);
    }
  };
  
  return (
    <div className="connection-tool">
      <div className="connection-tool-header">
        <span>Create Connection</span>
        <button onClick={onCancel}>✕</button>
      </div>
      
      <div className="connection-tool-body">
        <div className="connection-step">
          <label>From:</label>
          <RoleBadge roleId={fromRoleId} />
        </div>
        
        <div className="connection-step">
          <label>To:</label>
          {toRoleId ? (
            <RoleBadge roleId={toRoleId} />
          ) : (
            <span className="hint">Click on a role in the canvas</span>
          )}
        </div>
        
        <div className="connection-step">
          <label>Type:</label>
          <Select
            value={type}
            onChange={e => setType(e.target.value as RelativeType)}
            options={[
              { value: 'delegates_to', label: 'Delegates To' },
              { value: 'spawned_by', label: 'Spawned By' },
              { value: 'can_observe', label: 'Can Observe' },
              { value: 'collaborates_with', label: 'Collaborates With' },
            ]}
          />
        </div>
        
        <div className="connection-tool-footer">
          <Button variant="secondary" onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={handleConfirm}
            disabled={!toRoleId}
          >
            Create Connection
          </Button>
        </div>
      </div>
    </div>
  );
};
```


______________________________________________________________________

## Итог

**Team Builder предоставляет:**

✅ **Визуальный редактор** — drag \& drop сегментов и ролей\
✅ **Создание связей** — кликом между ролями\
✅ **Детальная конфигурация** — плагины, skills, модель, ресурсы\
✅ **Валидация** — проверка на ошибки и предупреждения\
✅ **Сохранение в Library** — переиспользование конфигураций\
✅ **Импорт/Экспорт** — YAML/JSON\
✅ **История версий** — restore предыдущих топологий\
✅ **Real-time updates** — WebSocket для совместной работы

**API полностью покрывает:**

- CRUD для сегментов, ролей, связей
- Layout (позиции на холсте)
- Конфигурация ролей (agent_spec, plugins, skills)
- Валидация топологии
- Версионирование
