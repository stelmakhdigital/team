// R1 (редизайн UI, референс openRIG): граф топологии на React Flow.
// Display-режим по умолчанию (pan/zoom/fit), edit-режим — R2 (drag/connect).
import { useMemo } from 'react';
import {
  Background,
  BackgroundVariant,
  Controls,
  MarkerType,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  type Edge,
  type Node,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import type { GetTopologyResponse, Relative } from '../../types/api';
import { computeTopologyLayout, ROLE_H, ROLE_W, type TopologyLayoutResult } from './layout/autoLayout';
import { RoleNode } from './nodes/RoleNode';
import { SegmentGroupNode } from './nodes/SegmentGroupNode';
import { TaskEdge } from './edges/TaskEdge';
import '../../topology.css';

const nodeTypes = { role: RoleNode, segment: SegmentGroupNode };
const edgeTypes = { task: TaskEdge };

const KIND_COLORS: Record<string, string> = {
  delegates_to: '#10b981',
  spawned_by: '#60a5fa',
  can_observe: '#f59e0b',
  collaborates_with: '#a78bfa',
};

export type TopologySelection =
  | { type: 'role'; id: number }
  | { type: 'segment'; id: number }
  | { type: 'relative'; id: number }
  | null;

interface Props {
  data: GetTopologyResponse;
  editMode?: boolean;
  selection?: TopologySelection;
  onSelect?: (sel: TopologySelection) => void;
  onRoleMoved?: (roleId: number, pos: { x: number; y: number }) => void; // абсолютные координаты
  onSegmentMoved?: (segmentId: number, pos: { x: number; y: number }) => void;
}

interface InnerProps extends Props {
  tl: TopologyLayoutResult;
}

function TopologyCanvasInner({
  data,
  editMode = false,
  selection = null,
  onSelect,
  onRoleMoved,
  onSegmentMoved,
  tl,
}: InnerProps) {
  const { segments, roles, relatives, layout } = data;

  const { nodes, edges } = useMemo(() => {
    const segNodes: Node[] = segments.map((s) => {
      const p = tl.segments.get(s.id);
      const count = roles.filter((r) => r.segment_id === s.id).length;
      return {
        id: `seg:${s.id}`,
        type: 'segment',
        position: p ? { x: p.x, y: p.y } : { x: 0, y: 0 },
        width: p?.width,
        height: p?.height,
        data: { segment: s, count, selected: selection?.type === 'segment' && selection.id === s.id },
        draggable: editMode,
        selectable: true,
      };
    });

    const roleNodes: Node[] = roles.map((r) => {
      const p = tl.roles.get(r.id);
      const parent = p?.parentId ?? null;
      return {
        id: `role:${r.id}`,
        type: 'role',
        position: p ? { x: p.x, y: p.y } : { x: 0, y: 0 },
        width: ROLE_W,
        height: ROLE_H,
        parentId: parent != null ? `seg:${parent}` : undefined,
        extent: parent != null ? ('parent' as const) : undefined,
        data: { role: r, selected: selection?.type === 'role' && selection.id === r.id, editMode },
        draggable: editMode,
        selectable: true,
        connectable: editMode,
        zIndex: 1,
      };
    });

    const relEdges: Edge[] = relatives.map((rel: Relative) => {
      const color = KIND_COLORS[rel.type] ?? '#10b981';
      return {
        id: `rel:${rel.id}`,
        type: 'task',
        source: `role:${rel.from_role_id}`,
        target: `role:${rel.to_role_id}`,
        data: { kind: rel.type, selected: selection?.type === 'relative' && selection.id === rel.id },
        markerEnd: { type: MarkerType.ArrowClosed, color, width: 18, height: 18 },
        selectable: true,
      };
    });

    return { nodes: [...segNodes, ...roleNodes], edges: relEdges };
  }, [segments, roles, relatives, layout, selection, editMode, tl]);

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
      fitView
      fitViewOptions={{ padding: 0.2 }}
      minZoom={0.2}
      maxZoom={2.5}
      nodesDraggable={editMode}
      nodesConnectable={editMode}
      elementsSelectable
      deleteKeyCode={null}
      onNodeClick={(_e, n) => {
        if (n.type === 'role') onSelect?.({ type: 'role', id: Number(n.id.slice(5)) });
        else if (n.type === 'segment') onSelect?.({ type: 'segment', id: Number(n.id.slice(4)) });
      }}
      onEdgeClick={(_e, ed) => onSelect?.({ type: 'relative', id: Number(ed.id.slice(4)) })}
      onPaneClick={() => onSelect?.(null)}
      onNodeDragStop={(_e, n) => {
        const roleId = Number(n.id.slice(5));
        if (n.type === 'role') {
          const p = tl.roles.get(roleId);
          const sp = p?.parentId != null ? tl.segments.get(p.parentId) : undefined;
          if (sp) onRoleMoved?.(roleId, { x: n.position.x + sp.x, y: n.position.y + sp.y });
        } else if (n.type === 'segment') {
          onSegmentMoved?.(Number(n.id.slice(4)), { x: n.position.x, y: n.position.y });
        }
      }}
      proOptions={{ hideAttribution: true }}
    >
      <Background variant={BackgroundVariant.Dots} gap={24} size={1} color="rgba(148, 163, 184, 0.18)" />
      <Controls showInteractive={false} className="topo-controls" />
      <MiniMap
        className="topo-minimap"
        nodeColor={(n) => (n.type === 'segment' ? 'rgba(56, 189, 248, 0.15)' : '#10b981')}
        maskColor="rgba(2, 6, 23, 0.72)"
        pannable
        zoomable
      />
    </ReactFlow>
  );
}

export function TopologyCanvas(props: Props) {
  const { segments, roles, relatives, layout } = props.data;
  const tl = useMemo(
    () => computeTopologyLayout(segments, roles, relatives, layout),
    [segments, roles, relatives, layout],
  );
  return (
    <ReactFlowProvider>
      <TopologyCanvasInner {...props} tl={tl} />
    </ReactFlowProvider>
  );
}

export default TopologyCanvas;
