// VersionPicker — right-rail type-ahead over a doc's versions. Absent when
// there is exactly one (docs/design/serve-plans-page.md "Reading a slug").
// Matching flattens every checkout's branch in a version's set, so typing a
// name that isn't the entry's own label still finds it — the label is only
// the representative name.
import { useMemo, useState } from "react";
import { Combobox, createListCollection } from "@ark-ui/react";
import { formatDate, type PlanCheckout, type PlanDocVersion } from "../../utils/plansApi";
import "./style.css";

function representativeCheckout(version: PlanDocVersion): PlanCheckout | undefined {
  return version.checkouts.find((c) => c.branch === version.label) ?? version.checkouts[0];
}

function matchesQuery(version: PlanDocVersion, query: string): boolean {
  if (!query) return true;
  const q = query.toLowerCase();
  return version.checkouts.some((c) => c.branch.toLowerCase().includes(q));
}

interface VersionPickerProps {
  versions: PlanDocVersion[];
  /** The version currently rendered — read from the resolved checkout, never
      from a stored preference, so the picker can never diverge from it. */
  active: PlanDocVersion;
  onSelect: (version: PlanDocVersion) => void;
}

export function VersionPicker({ versions, active, onSelect }: VersionPickerProps) {
  const [query, setQuery] = useState("");
  const [inputValue, setInputValue] = useState(active.label);

  const filtered = useMemo(() => versions.filter((v) => matchesQuery(v, query)), [versions, query]);

  const collection = useMemo(
    () =>
      createListCollection({
        items: filtered,
        itemToString: (v: PlanDocVersion) => v.label,
        itemToValue: (v: PlanDocVersion) => v.sha,
      }),
    [filtered],
  );

  if (versions.length <= 1) return null;

  return (
    <Combobox.Root
      key={active.sha}
      collection={collection}
      inputValue={inputValue}
      openOnClick
      selectionBehavior="replace"
      className="vpicker"
      onInputValueChange={(details) => {
        setInputValue(details.inputValue);
        setQuery(details.inputValue);
      }}
      onValueChange={(details) => {
        const picked = collection.find(details.value[0]);
        if (picked) onSelect(picked);
      }}
    >
      <Combobox.Label className="rail-slot-label">Version</Combobox.Label>
      <Combobox.Control className="vpick">
        <Combobox.Input
          className="vpick-input"
          onFocus={() => {
            setInputValue("");
            setQuery("");
          }}
          onBlur={() => {
            if (!inputValue) setInputValue(active.label);
          }}
        />
        {active.isMain ? <span className="badge">merged</span> : null}
        <Combobox.Trigger className="cue">&#9662;</Combobox.Trigger>
      </Combobox.Control>
      <Combobox.Positioner>
        <Combobox.Content className="vmenu">
          {filtered.length === 0 ? (
            <Combobox.Empty className="vempty">no matches</Combobox.Empty>
          ) : (
            filtered.map((v) => {
              const rep = representativeCheckout(v);
              const created = rep?.created ? formatDate(rep.created) : "";
              const updated = rep?.fileMtime ? formatDate(rep.fileMtime) : "";
              return (
                <Combobox.Item key={v.sha} item={v} className={v.sha === active.sha ? "vopt on" : "vopt"}>
                  <Combobox.ItemText>
                    <div className="vname">
                      <span className="vopt-dot" data-filled={v.isMain ? "" : undefined} />
                      {v.label}
                      {v.isMain ? <span className="badge">merged</span> : null}
                      {rep?.outsideRoot ? <span className="badge out">outside root</span> : null}
                    </div>
                    {rep ? <div className="vpath">{rep.path}</div> : null}
                    {created || updated ? (
                      <div className="vmeta">
                        {created ? `created ${created}` : null}
                        {created && updated ? " · " : null}
                        {updated ? `updated ${updated}` : null}
                      </div>
                    ) : null}
                  </Combobox.ItemText>
                </Combobox.Item>
              );
            })
          )}
        </Combobox.Content>
      </Combobox.Positioner>
    </Combobox.Root>
  );
}
