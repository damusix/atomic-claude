// NavTree — the left nav as an Ark TreeView folder tree, one collapsible
// section per /api/nav group (Realm/Repos/Concerns/... or Docs, depending on
// scope). Folder nodes (Buckets, repo-scope docs subdirectories) are
// TreeView.Branch; leaves are TreeView.Item that route via React Router.
import { useEffect } from "react";
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

function NavLeaf({ node }: { node: NavNode }) {
  return (
    <TreeView.Item>
      <TreeView.ItemText>
        <Link to={navNodeHref(node.relpath ?? "")} className="nav-item">
          {node.label}
          {node.stale ? (
            <span className="nav-badge nav-badge-stale" title="stale" aria-label="stale">
              ●
            </span>
          ) : null}
        </Link>
      </TreeView.ItemText>
    </TreeView.Item>
  );
}

function NavTreeNode({ model, indexPath }: { model: TreeNodeModel; indexPath: number[] }) {
  const isFolder = model.children !== undefined;
  return (
    <TreeView.NodeProvider node={model} indexPath={indexPath}>
      {isFolder ? (
        <TreeView.Branch>
          <TreeView.BranchControl>
            <TreeView.BranchText className="nav-item nav-folder">
              {model.node.label}
              {model.node.stale ? (
                <span className="nav-badge nav-badge-stale" title="stale" aria-label="stale">
                  ●
                </span>
              ) : null}
            </TreeView.BranchText>
          </TreeView.BranchControl>
          <TreeView.BranchContent>
            {model.children?.map((child, i) => (
              <NavTreeNode key={child.id} model={child} indexPath={[...indexPath, i]} />
            ))}
          </TreeView.BranchContent>
        </TreeView.Branch>
      ) : (
        <NavLeaf node={model.node} />
      )}
    </TreeView.NodeProvider>
  );
}

function NavGroupTree({ group }: { group: NavGroup }) {
  const roots = group.items.map((item, i) => toTreeNodeModel(item, group.name, i));
  const collection = createTreeCollection<TreeNodeModel>({
    nodeToValue: (n) => n.id,
    nodeToString: (n) => n.node.label,
    rootNode: { id: group.name, node: { label: group.name }, children: roots },
  });

  return (
    <section className="nav-section" aria-label={group.name}>
      <div className="nav-group">{group.name}</div>
      {roots.length === 0 ? (
        <span className="nav-empty">nothing here yet</span>
      ) : (
        <TreeView.Root collection={collection}>
          <TreeView.Tree>
            {roots.map((model, i) => (
              <NavTreeNode key={model.id} model={model} indexPath={[i]} />
            ))}
          </TreeView.Tree>
        </TreeView.Root>
      )}
    </section>
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
      {data.groups.map((group) => (
        <NavGroupTree key={group.name} group={group} />
      ))}
    </nav>
  );
}
