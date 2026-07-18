// NavTree — the left nav as ONE Ark TreeView: every /api/nav group
// (Realm/Repos/Concerns/... or Docs, depending on scope) is a top-level
// collapsible branch (expanded by default), folder nodes (Buckets, repo-scope
// docs subdirectories) are nested branches, leaves route via React Router.
import { useEffect, useMemo } from "react";
import { TreeView, createTreeCollection } from "@ark-ui/react";
import { Link } from "react-router";
import { useApi } from "../../utils/api";
import { events } from "../../utils/events";
import { navNodeHref, type NavGroup, type NavNode, type NavResponse } from "./types";
import "./style.css";

// Ark's TreeCollection is generic over any node shape carrying a stable id
// and optional children — group items have no id from the server, so this
// wraps each NavNode with one synthesized from its position in the tree.
interface TreeNodeModel {
  id: string;
  node: NavNode;
  group?: boolean;
  children?: TreeNodeModel[];
}

function toTreeNodeModel(node: NavNode, idPrefix: string, index: number): TreeNodeModel {
  const id = `${idPrefix}/${index}:${node.label}`;
  return {
    id,
    node,
    children: node.children?.map((child, childIndex) => toTreeNodeModel(child, id, childIndex)),
  };
}

// group.items is `null` (not `[]`) when the API's Go []navNodeJSON slice is
// nil — e.g. the repo-scope "Code" group placeholder — since Go's
// encoding/json marshals a nil slice as JSON null, not an empty array.
function groupToTreeNodeModel(group: NavGroup): TreeNodeModel {
  return {
    id: `group:${group.name}`,
    node: { label: group.name },
    group: true,
    children: (group.items ?? []).map((item, i) => toTreeNodeModel(item, group.name, i)),
  };
}

function StaleBadge({ node }: { node: NavNode }) {
  if (!node.stale) return null;
  return (
    <span className="nav-badge nav-badge-stale" title="stale" aria-label="stale">
      ●
    </span>
  );
}

function NavLeaf({ node }: { node: NavNode }) {
  return (
    <TreeView.Item>
      <TreeView.ItemText>
        <Link to={navNodeHref(node.relpath ?? "")} className="nav-item">
          {node.label}
          <StaleBadge node={node} />
        </Link>
      </TreeView.ItemText>
    </TreeView.Item>
  );
}

function NavTreeNode({ model, indexPath }: { model: TreeNodeModel; indexPath: number[] }) {
  const { children } = model;
  return (
    <TreeView.NodeProvider node={model} indexPath={indexPath}>
      {children !== undefined ? (
        <TreeView.Branch>
          <TreeView.BranchControl className={model.group ? "nav-group-control" : undefined}>
            <TreeView.BranchText className={model.group ? "nav-group" : "nav-item nav-folder"}>
              {model.node.label}
              <StaleBadge node={model.node} />
            </TreeView.BranchText>
          </TreeView.BranchControl>
          <TreeView.BranchContent className="nav-branch-content">
            {children.length === 0 && model.group ? (
              <span className="nav-empty">nothing here yet</span>
            ) : (
              children.map((child, i) => (
                <NavTreeNode key={child.id} model={child} indexPath={[...indexPath, i]} />
              ))
            )}
          </TreeView.BranchContent>
        </TreeView.Branch>
      ) : (
        <NavLeaf node={model.node} />
      )}
    </TreeView.NodeProvider>
  );
}

export function NavTree() {
  const { get } = useApi();
  const { data, loading, failure, refetch } = get<NavResponse>("/nav");

  // Live-reload reconcile (spec Flow): nav has no conditional — every
  // realm.changed message refetches it, unlike page/rail's changed-list
  // check (see pages/Page and components/rail/Rail).
  useEffect(() => {
    return events.on("realm.changed", () => refetch());
  }, [refetch]);

  const groups = useMemo(() => (data?.groups ?? []).map(groupToTreeNodeModel), [data]);
  const collection = useMemo(
    () =>
      createTreeCollection<TreeNodeModel>({
        nodeToValue: (n) => n.id,
        nodeToString: (n) => n.node.label,
        rootNode: { id: "root", node: { label: "root" }, children: groups },
      }),
    [groups],
  );

  if (loading && !data) {
    return (
      <nav id="nav-pane" aria-label="Navigation">
        <div className="nav-section-title">Loading…</div>
      </nav>
    );
  }

  if (failure || !data) {
    return (
      <nav id="nav-pane" aria-label="Navigation">
        <div className="nav-section-title">Navigation unavailable</div>
      </nav>
    );
  }

  return (
    <nav id="nav-pane" aria-label="Navigation">
      <TreeView.Root
        collection={collection}
        defaultExpandedValue={groups.map((g) => g.id)}
        aria-label="Navigation tree"
      >
        <TreeView.Tree>
          {groups.map((model, i) => (
            <NavTreeNode key={model.id} model={model} indexPath={[i]} />
          ))}
        </TreeView.Tree>
      </TreeView.Root>
    </nav>
  );
}
