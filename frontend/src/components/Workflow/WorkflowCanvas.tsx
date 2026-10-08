// Workflow-канвас (R6.4): React Flow — pan/zoom/миникарта, нативный drag (без
// HTML5-DnD лагов), responsive 100%×100% (старый фикс 1400×900 убран).
import { useCallback, useMemo } from 'react';
import {
  ReactFlow,
  Background,
  BackgroundVariant,
  MiniMap,
  Controls,
  MarkerType,
  type Connection,
  type Edge,
  type Node,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import '../../topology.css'; // wf-* стили + dark-тема react-flow (общие правила)
import { BlockNode } from './nodes/BlockNode';
import { BLOCK_H, BLOCK_W, workflowLevels } from './layout';
import type { WorkflowBlock, WorkflowConnection } from '../../types/api';

export interface WorkflowCanvasProps {
  blocks: WorkflowBlock[];
  connections: WorkflowConnection[];
  /** null — авто-layout; иначе позиции из layout (hybrid) */
  positions: Map<number, { x: number; y: number }> | null;
  selectedBlockId: number | null;
  selectedConnectionId: number | null;
  onBlockClick: (blockId: number | null) => void;
  onConnectionClick: (connectionId: number | null) => void;
  onBlockMoved: (blockId: number, pos: { x: number; y: number }) => void;
  onConnect: (fromBlockId: number, toBlockId: number) => void;
}

const nodeTypes = { block: BlockNode };

const CONDITION_COLORS: Record<string, string> = {
  yes: '#10b981',
  no: '#ef4444',
};

export default function WorkflowCanvas({
  blocks,
  connections,
  positions,
  selectedBlockId,
  selectedConnectionId,
  onBlockClick,
  onConnectionClick,
  onBlockMoved,
  onConnect,
}: WorkflowCanvasProps) {
  // без positions — авто-layout (топологические уровни); hybrid: сохранённые позиции побждают
  const effectivePositions = useMemo(() => {
    if (positions && positions.size > 0) return positions;
    const lv = workflowLevels(blocks, connections);
    const byLevel = new Map<number, number[]>();
    for (const b of blocks) {
      const l = lv.get(b.id) ?? 0;
      byLevel.set(l, [...(byLevel.get(l) ?? []), b.id]);
    }
    const m = new Map<number, { x: number; y: number }>();
    for (const [l, list] of byLevel) {
      list.forEach((id, i) => m.set(id, { x: l * (BLOCK_W + 64), y: i * (BLOCK_H + 36) }));
    }
    return m;
  }, [positions, blocks, connections]);

  const nodes: Node[] = useMemo(
    () =>
      blocks.map((b) => ({
        id: `blk:${b.id}`,
        type: 'block',
        position: effectivePositions.get(b.id) ?? { x: 0, y: 0 },
        width: BLOCK_W,
        height: BLOCK_H,
        data: { block: b, selected: selectedBlockId === b.id, editMode: true },
        draggable: true,
        selectable: true,
        connectable: true,
        zIndex: 1,
      })),
    [blocks, effectivePositions, selectedBlockId],
  );

  const edges: Edge[] = useMemo(
    () =>
      connections.map((c) => {
        const sel = selectedConnectionId === c.id;
        const color = c.condition ? CONDITION_COLORS[c.condition] ?? '#64748b' : sel ? '#38bdf8' : '#64748b';
        return {
          id: `conn:${c.id}`,
          source: `blk:${c.from_block_id}`,
          target: `blk:${c.to_block_id}`,
          type: 'default',
          animated: sel,
          label: c.condition || undefined,
          labelStyle: { fill: color, fontSize: 10, fontFamily: 'ui-monospace, monospace' },
          labelBgStyle: { fill: 'rgba(2, 6, 23, 0.85)' },
          style: { stroke: color, strokeWidth: sel ? 2.5 : 1.5 },
          markerEnd: { type: MarkerType.ArrowClosed, color },
          selected: sel,
        };
      }),
    [connections, selectedConnectionId],
  );

  const handleNodeDragStop = useCallback(
    (_e: unknown, node: Node) => {
      const id = Number(node.id.replace('blk:', ''));
      onBlockMoved(id, { x: Math.round(node.position.x), y: Math.round(node.position.y) });
    },
    [onBlockMoved],
  );

  const handleConnect = useCallback(
    (c: Connection) => {
      const from = Number(c.source?.replace('blk:', ''));
      const to = Number(c.target?.replace('blk:', ''));
      if (!Number.isNaN(from) && !Number.isNaN(to) && from !== to) onConnect(from, to);
    },
    [onConnect],
  );

  const handleNodeClick = useCallback(
    (_e: React.MouseEvent, node: Node) => {
      onBlockClick(Number(node.id.replace('blk:', '')));
    },
    [onBlockClick],
  );

  const handleEdgeClick = useCallback(
    (_e: React.MouseEvent, edge: Edge) => {
      onConnectionClick(Number(edge.id.replace('conn:', '')));
    },
    [onConnectionClick],
  );

  const handlePaneClick = useCallback(() => {
    onBlockClick(null);
    onConnectionClick(null);
  }, [onBlockClick, onConnectionClick]);

  return (
    <div className="workflow-canvas" aria-label="Workflow canvas">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodeDragStop={handleNodeDragStop}
        onConnect={handleConnect}
        onNodeClick={handleNodeClick}
        onEdgeClick={handleEdgeClick}
        onPaneClick={handlePaneClick}
        fitView
        fitViewOptions={{ padding: 0.15, maxZoom: 1 }}
        minZoom={0.2}
        maxZoom={2}
        proOptions={{ hideAttribution: true }}
      >
        <Background variant={BackgroundVariant.Dots} gap={18} size={1} color="rgba(148, 163, 184, 0.14)" />
        <Controls showInteractive={false} position="bottom-left" />
        <MiniMap
          position="bottom-right"
          nodeColor={(n) => (n.selected ? '#38bdf8' : '#1e293b')}
          maskColor="rgba(2, 6, 23, 0.7)"
        />
      </ReactFlow>
    </div>
  );
}
