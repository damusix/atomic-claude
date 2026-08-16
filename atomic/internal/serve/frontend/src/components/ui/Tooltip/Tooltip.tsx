// Tooltip — the app's one hover/focus label, over Ark's Tooltip primitive.
//
// The native `title` attribute is not a substitute: it waits about a second
// with no way to tune the delay, renders in an OS style no theme can touch,
// truncates long values, never appears on keyboard focus, and cannot be
// positioned. Everything the shell labels — rail mode glyphs, elided link
// paths, the scope chip — goes through here instead.
import type { ReactNode } from "react";
import { Tooltip as ArkTooltip } from "@ark-ui/react";
import "./style.css";

export function Tooltip({
  label,
  children,
  placement = "right",
  openDelay = 260,
}: {
  label: ReactNode;
  children: ReactNode;
  placement?: "top" | "right" | "bottom" | "left";
  openDelay?: number;
}) {
  if (!label) return <>{children}</>;

  return (
    <ArkTooltip.Root
      openDelay={openDelay}
      closeDelay={80}
      // Gutter clears the trigger rather than resting against it: a tooltip
      // touching its own trigger is what puts it under the pointer.
      positioning={{ placement, gutter: 12 }}
      // Zag closes on scroll by default, which on a trackpad means the
      // stray deltas a resting finger emits close the tooltip while the
      // pointer never leaves the trigger — so it reopens after openDelay and
      // flickers. Nothing here scrolls with the content: the header is fixed
      // and the icon rail is pinned, so their labels have no reason to
      // dismiss on scroll at all.
      closeOnScroll={false}
      // Without these the content of every tooltip sits in the DOM from first
      // render — two dozen hidden copies of link paths in the rail alone, each
      // a duplicate of the text its own row already shows.
      lazyMount
      unmountOnExit
    >
      <ArkTooltip.Trigger asChild>{children}</ArkTooltip.Trigger>
      <ArkTooltip.Positioner>
        <ArkTooltip.Content className="ui-tooltip">{label}</ArkTooltip.Content>
      </ArkTooltip.Positioner>
    </ArkTooltip.Root>
  );
}
