import { describe, expect, it } from "vitest";

import { oklchToRenderColor } from "@/lib/oklch";

describe("oklchToRenderColor", () => {
  it("converts the achromatic extremes exactly", () => {
    expect(oklchToRenderColor("1 0 0")).toEqual({ color: "rgb(255, 255, 255)", opacity: 1 });
    expect(oklchToRenderColor("0 0 0")).toEqual({ color: "rgb(0, 0, 0)", opacity: 1 });
  });

  it("converts the Dusk brand teal into the right corner of sRGB", () => {
    // oklch(0.72 0.118 178) — a mid-light cyan-green: green and blue dominate.
    const [r, g, b] = parse(oklchToRenderColor("0.72 0.118 178").color);
    expect(g).toBeGreaterThan(r);
    expect(b).toBeGreaterThan(r);
    expect(g).toBeGreaterThan(b);
    expect(g).toBeGreaterThan(150);
  });

  it("converts the Dusk primary violet into the right corner of sRGB", () => {
    // oklch(0.585 0.150 300) — violet: blue leads, red beats green.
    const [r, g, b] = parse(oklchToRenderColor("0.585 0.150 300").color);
    expect(b).toBeGreaterThan(r);
    expect(r).toBeGreaterThan(g);
  });

  it("keeps alpha out of the colour string, because the renderer wants it apart", () => {
    expect(oklchToRenderColor("1 0 0", 0.06)).toEqual({ color: "rgb(255, 255, 255)", opacity: 0.06 });
  });

  it("reads a full oklch() colour, including its own alpha", () => {
    expect(oklchToRenderColor("oklch(1 0 0 / 10%)")).toEqual({ color: "rgb(255, 255, 255)", opacity: 0.1 });
    expect(oklchToRenderColor("oklch(1 0 0 / 0.25)")).toEqual({ color: "rgb(255, 255, 255)", opacity: 0.25 });
    expect(oklchToRenderColor("oklch(0 0 0)")).toEqual({ color: "rgb(0, 0, 0)", opacity: 1 });
  });

  it("lets a colour's own alpha win over the argument", () => {
    expect(oklchToRenderColor("oklch(1 0 0 / 10%)", 0.9).opacity).toBe(0.1);
  });

  it("reads percentage lightness and chroma", () => {
    expect(oklchToRenderColor("oklch(100% 0 0)").color).toBe("rgb(255, 255, 255)");
  });

  it("clips an out-of-gamut colour instead of emitting a nonsense channel", () => {
    const [r, g, b] = parse(oklchToRenderColor("0.9 0.4 140").color); // far outside sRGB
    for (const channel of [r, g, b]) {
      expect(channel).toBeGreaterThanOrEqual(0);
      expect(channel).toBeLessThanOrEqual(255);
    }
  });

  it("passes anything it does not recognize straight through", () => {
    expect(oklchToRenderColor("#ff0000").color).toBe("#ff0000");
    expect(oklchToRenderColor("rgba(1, 2, 3, 0.5)").color).toBe("rgba(1, 2, 3, 0.5)");
    expect(oklchToRenderColor("color-mix(in oklch, white 12%, transparent)").color).toBe(
      "color-mix(in oklch, white 12%, transparent)",
    );
  });
});

function parse(rgb: string): [number, number, number] {
  const match = /^rgb\((\d+), (\d+), (\d+)\)$/.exec(rgb);
  if (!match) throw new Error(`not an rgb() string: ${rgb}`);
  return [Number(match[1]), Number(match[2]), Number(match[3])];
}
