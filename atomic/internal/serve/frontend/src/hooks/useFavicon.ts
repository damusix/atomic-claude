// useFavicon — draws the brand mark into the tab icon and animates it while
// the live-reload stream is connected.
//
// The tab is the one part of the app visible when the app is not: this turns
// "is my serve still watching the realm?" into something answerable from a
// background tab. Connected spins and breathes; anything else freezes the
// mark and drains it to grey, so a dead stream reads as dead at a glance.
//
// A favicon cannot be animated by CSS or an animated PNG that reacts to state,
// so each frame is drawn to a canvas and pushed to the <link> as a data URL.
import { useEffect } from "react";
import type { ConnState } from "./useLiveReload";

const SIZE = 64;
/** 30fps — the step between frames is small enough that the rotation reads as
    continuous rather than as a sequence of positions. */
const FRAME_MS = 33;
/** Seconds per revolution. Slow: the mark should register as alive in
    peripheral vision, not pull the eye to the tab. */
const SPIN_PERIOD = 14;
/** Seconds per breath, independent of the spin so the two never lock into a
    single obvious repeat. */
const PULSE_PERIOD = 5;

function iconLink(): HTMLLinkElement {
  const existing = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (existing) return existing;
  const link = document.createElement("link");
  link.rel = "icon";
  document.head.appendChild(link);
  return link;
}

function loadLogo(src: string): Promise<HTMLImageElement | null> {
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => resolve(null);
    img.src = src;
  });
}

export function useFavicon(connState: ConnState, src = "/logo.png") {
  useEffect(() => {
    let frame = 0;
    let timer: ReturnType<typeof setInterval> | null = null;
    let cancelled = false;
    let cleanupVisibility: (() => void) | null = null;

    const canvas = document.createElement("canvas");
    canvas.width = SIZE;
    canvas.height = SIZE;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    const link = iconLink();

    function draw(logo: HTMLImageElement, animate: boolean) {
      if (!ctx) return;
      ctx.clearRect(0, 0, SIZE, SIZE);
      ctx.save();

      const seconds = (frame * FRAME_MS) / 1000;
      // (1 - cos)/2 is a cosine ease-in-out over the cycle: the mark eases
      // into each extreme and back out, where a raw sine crosses the midpoint
      // at full speed and reads as a throb.
      const eased = (1 - Math.cos((seconds / PULSE_PERIOD) * Math.PI * 2)) / 2;
      const pulse = animate ? 0.9 + 0.1 * eased : 1;
      const spin = animate ? (seconds / SPIN_PERIOD) * Math.PI * 2 : 0;

      ctx.translate(SIZE / 2, SIZE / 2);
      ctx.rotate(spin);
      ctx.scale(pulse, pulse);
      // Disconnected reads as inert: no motion, no colour.
      ctx.filter = animate ? "none" : "grayscale(1)";
      ctx.globalAlpha = animate ? 1 : 0.45;
      ctx.drawImage(logo, -SIZE / 2, -SIZE / 2, SIZE, SIZE);

      ctx.restore();
      link.href = canvas.toDataURL("image/png");
    }

    void loadLogo(src).then((logo) => {
      if (cancelled || !logo) return;
      const animate = connState === "live";
      draw(logo, animate);
      if (!animate) return;

      function start() {
        if (timer) return;
        timer = setInterval(() => {
          frame += 1;
          draw(logo as HTMLImageElement, true);
        }, FRAME_MS);
      }
      function stop() {
        if (!timer) return;
        clearInterval(timer);
        timer = null;
      }
      // A hidden tab cannot show the animation; keeping the interval alive
      // there would be pure background work.
      function onVisibility() {
        if (document.hidden) stop();
        else start();
      }

      if (!document.hidden) start();
      document.addEventListener("visibilitychange", onVisibility);
      cleanupVisibility = () => document.removeEventListener("visibilitychange", onVisibility);
    });

    return () => {
      cancelled = true;
      if (timer) clearInterval(timer);
      cleanupVisibility?.();
    };
  }, [connState, src]);
}
