import { useState } from "react";
import type { SankeyLink, SankeyNode } from "./parser";
import { formatCurrency } from "./parser";

interface PositionedNode extends SankeyNode {
  x: number;
  y: number;
  width: number;
  height: number;
}

interface PositionedLink extends SankeyLink {
  path: string;
  sourceNode: PositionedNode;
  targetNode: PositionedNode;
  gradientId: string;
}

interface HoverInfo {
  title: string;
  subtitle: string;
  x: number;
  y: number;
}

export function SankeyDiagram({
  nodes,
  links,
  currency,
}: {
  nodes: SankeyNode[];
  links: SankeyLink[];
  currency: string;
}) {
  const [hoveredNodeId, setHoveredNodeId] = useState<string | null>(null);
  const [hoveredLinkId, setHoveredLinkId] = useState<string | null>(null);
  const [hoverInfo, setHoverInfo] = useState<HoverInfo | null>(null);

  const svgWidth = 920;
  const svgHeight = 360;
  const topPadding = 45;
  const bottomPadding = 25;
  const usableHeight = svgHeight - topPadding - bottomPadding;

  const colWidths = [190, 160, 190];
  const colXs = [40, 380, 690];

  // Group nodes by column
  const col0Nodes = nodes.filter((n) => n.column === 0);
  const col1Nodes = nodes.filter((n) => n.column === 1);
  const col2Nodes = nodes.filter((n) => n.column === 2);

  const positionedNodes = new Map<string, PositionedNode>();

  function positionColumn(colNodes: SankeyNode[], colIndex: number) {
    if (colNodes.length === 0) return;
    const totalVal = colNodes.reduce((sum, n) => sum + n.value, 0) || 1;
    const minHeight = 28;
    const gap = 12;

    const availableForHeights = Math.max(
      100,
      usableHeight - (colNodes.length - 1) * gap,
    );
    const heights = colNodes.map((n) =>
      Math.max(minHeight, (n.value / totalVal) * availableForHeights),
    );
    const totalRenderedHeight =
      heights.reduce((sum, h) => sum + h, 0) + (colNodes.length - 1) * gap;

    let startY =
      topPadding + Math.max(0, (usableHeight - totalRenderedHeight) / 2);

    colNodes.forEach((node, idx) => {
      const h = heights[idx] ?? minHeight;
      const x = colXs[colIndex] ?? 40;
      const w = colWidths[colIndex] ?? 160;
      positionedNodes.set(node.id, {
        ...node,
        x,
        y: startY,
        width: w,
        height: h,
      });
      startY += h + gap;
    });
  }

  positionColumn(col0Nodes, 0);
  positionColumn(col1Nodes, 1);
  positionColumn(col2Nodes, 2);

  // Layout links with smooth Bezier curves
  const sourceOffsets = new Map<string, number>();
  const targetOffsets = new Map<string, number>();

  const positionedLinks: PositionedLink[] = [];

  links.forEach((link, idx) => {
    const sNode = positionedNodes.get(link.source);
    const tNode = positionedNodes.get(link.target);
    if (!sNode || !tNode) return;

    const sOffset = sourceOffsets.get(link.source) ?? 0;
    const tOffset = targetOffsets.get(link.target) ?? 0;

    const sRatio = sNode.value > 0 ? link.value / sNode.value : 1;
    const tRatio = tNode.value > 0 ? link.value / tNode.value : 1;

    const sHeight = Math.max(3, sNode.height * sRatio);
    const tHeight = Math.max(3, tNode.height * tRatio);

    const x0 = sNode.x + sNode.width;
    const y0Top = sNode.y + sOffset;
    const y0Bottom = y0Top + sHeight;

    const x1 = tNode.x;
    const y1Top = tNode.y + tOffset;
    const y1Bottom = y1Top + tHeight;

    sourceOffsets.set(link.source, sOffset + sHeight);
    targetOffsets.set(link.target, tOffset + tHeight);

    const curvature = 0.5;
    const cx0 = x0 + (x1 - x0) * curvature;
    const cx1 = x1 - (x1 - x0) * curvature;

    const path = `M ${x0} ${y0Top} C ${cx0} ${y0Top}, ${cx1} ${y1Top}, ${x1} ${y1Top} L ${x1} ${y1Bottom} C ${cx1} ${y1Bottom}, ${cx0} ${y0Bottom}, ${x0} ${y0Bottom} Z`;

    positionedLinks.push({
      ...link,
      path,
      sourceNode: sNode,
      targetNode: tNode,
      gradientId: `link-grad-${idx}`,
    });
  });

  const getNodeColor = (type: SankeyNode["type"]) => {
    switch (type) {
      case "income":
        return { fill: "#059669", stroke: "#10b981", text: "#ecfdf5" };
      case "pool":
        return { fill: "#0284c7", stroke: "#06b6d4", text: "#f0f9ff" };
      case "expense":
        return { fill: "#e11d48", stroke: "#f43f5e", text: "#fff1f2" };
      case "savings":
        return { fill: "#7c3aed", stroke: "#a78bfa", text: "#f5f3ff" };
      default:
        return { fill: "#475569", stroke: "#64748b", text: "#f8fafc" };
    }
  };

  return (
    <div className="relative w-full overflow-hidden rounded-xl border border-base-content/10 bg-base-200/50 p-4 shadow-sm">
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-base-content/70">
          Money Flow (Sankey Diagram)
        </h3>
        <span className="text-[11px] text-base-content/50">
          Hover over nodes or flow lines for breakdown
        </span>
      </div>

      <div className="relative w-full overflow-x-auto">
        <svg
          aria-label="Money Flow Sankey Diagram"
          className="mx-auto block select-none"
          height={svgHeight}
          role="img"
          viewBox={`0 0 ${svgWidth} ${svgHeight}`}
          width="100%"
        >
          <defs>
            {/* Gradients for links */}
            {positionedLinks.map((link) => {
              const sColor = getNodeColor(link.sourceNode.type).stroke;
              const tColor = getNodeColor(link.targetNode.type).stroke;
              return (
                <linearGradient
                  id={link.gradientId}
                  key={link.gradientId}
                  x1="0%"
                  x2="100%"
                  y1="0%"
                  y2="0%"
                >
                  <stop offset="0%" stopColor={sColor} />
                  <stop offset="100%" stopColor={tColor} />
                </linearGradient>
              );
            })}
          </defs>

          {/* Column Header Titles */}
          <text
            className="fill-base-content/60 font-semibold"
            fontSize="11"
            letterSpacing="1"
            x={colXs[0]}
            y="24"
          >
            INFLOW (SOURCES)
          </text>
          <text
            className="fill-base-content/60 font-semibold"
            fontSize="11"
            letterSpacing="1"
            x={colXs[1]}
            y="24"
          >
            ACCOUNT POOL / RAILS
          </text>
          <text
            className="fill-base-content/60 font-semibold"
            fontSize="11"
            letterSpacing="1"
            x={colXs[2]}
            y="24"
          >
            OUTFLOW / ALLOCATIONS
          </text>

          {/* Links / Flows */}
          <g className="links">
            {positionedLinks.map((link) => {
              const isNodeHovered =
                hoveredNodeId === link.source || hoveredNodeId === link.target;
              const isLinkHovered = hoveredLinkId === link.gradientId;
              const isActive = isLinkHovered || isNodeHovered;
              const opacity =
                hoveredLinkId || hoveredNodeId ? (isActive ? 0.75 : 0.15) : 0.4;

              return (
                // biome-ignore lint/a11y/noStaticElementInteractions: Interactive SVG flow element
                <path
                  key={link.gradientId}
                  className="transition-opacity duration-150 cursor-pointer"
                  d={link.path}
                  fill={`url(#${link.gradientId})`}
                  fillOpacity={opacity}
                  onMouseEnter={(e) => {
                    setHoveredLinkId(link.gradientId);
                    setHoverInfo({
                      title: `${link.sourceNode.name} → ${link.targetNode.name}`,
                      subtitle: formatCurrency(link.value, currency),
                      x: e.clientX,
                      y: e.clientY,
                    });
                  }}
                  onMouseMove={(e) => {
                    setHoverInfo((prev) =>
                      prev ? { ...prev, x: e.clientX, y: e.clientY } : null,
                    );
                  }}
                  onMouseLeave={() => {
                    setHoveredLinkId(null);
                    setHoverInfo(null);
                  }}
                />
              );
            })}
          </g>

          {/* Nodes */}
          <g className="nodes">
            {Array.from(positionedNodes.values()).map((node) => {
              const colors = getNodeColor(node.type);
              const isHovered = hoveredNodeId === node.id;

              return (
                // biome-ignore lint/a11y/noStaticElementInteractions: Interactive SVG node element
                <g
                  key={node.id}
                  className="cursor-pointer transition-transform duration-150"
                  onMouseEnter={(e) => {
                    setHoveredNodeId(node.id);
                    setHoverInfo({
                      title: node.name,
                      subtitle: `${formatCurrency(node.value, currency)} (${node.type})`,
                      x: e.clientX,
                      y: e.clientY,
                    });
                  }}
                  onMouseMove={(e) => {
                    setHoverInfo((prev) =>
                      prev ? { ...prev, x: e.clientX, y: e.clientY } : null,
                    );
                  }}
                  onMouseLeave={() => {
                    setHoveredNodeId(null);
                    setHoverInfo(null);
                  }}
                >
                  <rect
                    fill={colors.fill}
                    filter={
                      isHovered
                        ? "drop-shadow(0 2px 8px rgba(0,0,0,0.3))"
                        : undefined
                    }
                    height={node.height}
                    rx="6"
                    ry="6"
                    stroke={colors.stroke}
                    strokeWidth={isHovered ? 2 : 1}
                    width={node.width}
                    x={node.x}
                    y={node.y}
                  />

                  {/* Node label text */}
                  <text
                    className="font-medium"
                    fill={colors.text}
                    fontSize={node.height > 38 ? "12" : "11"}
                    x={node.x + 10}
                    y={node.y + (node.height > 38 ? 16 : 14)}
                  >
                    {node.name.length > 20
                      ? `${node.name.slice(0, 19)}…`
                      : node.name}
                  </text>

                  {/* Node amount text */}
                  <text
                    className="font-bold opacity-90"
                    fill={colors.text}
                    fontSize={node.height > 38 ? "11" : "10"}
                    x={node.x + 10}
                    y={node.y + (node.height > 38 ? 32 : node.height - 5)}
                  >
                    {formatCurrency(node.value, currency)}
                  </text>
                </g>
              );
            })}
          </g>
        </svg>
      </div>

      {/* Floating hover tooltip */}
      {hoverInfo && (
        <div
          className="pointer-events-none fixed z-50 -translate-x-1/2 -translate-y-full rounded-lg bg-neutral px-3 py-1.5 text-xs text-neutral-content shadow-xl"
          style={{
            left: `${hoverInfo.x}px`,
            top: `${hoverInfo.y - 10}px`,
          }}
        >
          <div className="font-semibold">{hoverInfo.title}</div>
          <div className="text-[11px] opacity-80">{hoverInfo.subtitle}</div>
        </div>
      )}
    </div>
  );
}
