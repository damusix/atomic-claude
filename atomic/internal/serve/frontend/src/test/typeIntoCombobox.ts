import userEvent from "@testing-library/user-event";

/**
 * Types into an Ark UI combobox input with `userEvent.type`'s click-then-type
 * semantics, but one key per call so every keystroke is followed by an
 * `act()` flush.
 *
 * Zag defers every `send()` to a microtask, so the `input` handler returns
 * before `onInputValueChange` fires and React's controlled-value restore
 * resets the DOM to the stale value; the real update then lands as a scheduled
 * render. A single `userEvent.type(el, "abc")` yields between keys with a
 * `setTimeout(0)` that races that render task — the order differs between bun
 * on macOS and Linux, and on Linux keystrokes get dropped (`deslop` arrives as
 * `desop`). A browser never sees this: the next key is a later task and the
 * render always wins. Plain React inputs are unaffected, since React flushes
 * discrete events synchronously; use `userEvent.type` there.
 */
export async function typeIntoCombobox(el: Element, text: string): Promise<void> {
  await userEvent.click(el);
  for (const ch of text) {
    await userEvent.type(el, ch, { skipClick: true });
  }
}
