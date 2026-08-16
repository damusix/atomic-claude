// JsonValue — a JSON frontmatter value, highlighted and height-capped.
//
// These values are arbitrarily long (a sources: list can run to dozens of
// entries), and rendering one in full pushes everything below it out of the
// rail. Capped to a preview with a dialog for the whole thing.
import { useState } from "react";
import { Dialog } from "@ark-ui/react";

/** Token classes are assigned by shape, not by a parser: the input is already
    valid JSON from the server, so a scan is enough to colour it and cheaper
    than pulling in a highlighter for a rail slot. */
function tokenize(json: string) {
  const pattern =
    /("(?:\\.|[^"\\])*"\s*:)|("(?:\\.|[^"\\])*")|(\b-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b)|(\btrue\b|\bfalse\b)|(\bnull\b)|([{}[\],])/g;

  const out: { text: string; kind: string }[] = [];
  let last = 0;

  for (const match of json.matchAll(pattern)) {
    const index = match.index ?? 0;
    if (index > last) out.push({ text: json.slice(last, index), kind: "plain" });

    const [text, key, str, num, bool, nul, punct] = match;
    let kind = "plain";
    if (key) kind = "key";
    else if (str) kind = "string";
    else if (num) kind = "number";
    else if (bool) kind = "boolean";
    else if (nul) kind = "null";
    else if (punct) kind = "punct";

    out.push({ text, kind });
    last = index + text.length;
  }

  if (last < json.length) out.push({ text: json.slice(last), kind: "plain" });
  return out;
}

function Highlighted({ json }: { json: string }) {
  return (
    <>
      {tokenize(json).map((token, i) => (
        <span key={i} className={`json-${token.kind}`}>
          {token.text}
        </span>
      ))}
    </>
  );
}

/** Pretty-print when the value parses; otherwise show it verbatim rather
    than dropping a value we failed to understand. */
function pretty(value: string): string {
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
}

export function JsonValue({ propKey, value }: { propKey: string; value: string }) {
  const [open, setOpen] = useState(false);
  const formatted = pretty(value);

  return (
    <div className="rail-json">
      <pre className="rail-prop-json">
        <code>
          <Highlighted json={formatted} />
        </code>
      </pre>
      <button type="button" className="rail-json-expand" onClick={() => setOpen(true)}>
        expand
      </button>

      <Dialog.Root open={open} onOpenChange={(e) => setOpen(e.open)} lazyMount unmountOnExit>
        <Dialog.Backdrop className="rail-json-backdrop" />
        <Dialog.Positioner className="rail-json-positioner">
          <Dialog.Content className="rail-json-dialog">
            <header className="rail-json-dialog-head">
              <Dialog.Title className="rail-json-dialog-title">{propKey}</Dialog.Title>
              <Dialog.CloseTrigger className="rail-json-dialog-close" aria-label="Close">
                ✕
              </Dialog.CloseTrigger>
            </header>
            <pre className="rail-json-dialog-body">
              <code>
                <Highlighted json={formatted} />
              </code>
            </pre>
          </Dialog.Content>
        </Dialog.Positioner>
      </Dialog.Root>
    </div>
  );
}
