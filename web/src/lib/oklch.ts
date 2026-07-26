// OKLCH → sRGB, for renderers that cannot read CSS colours themselves.
//
// The Dusk palette is OKLCH end to end (ADR-0009), which is fine everywhere the
// browser does the painting. Canvas-based renderers (Cytoscape, FB-14) parse
// colour strings with their own parser — hex/rgb/hsl only — and silently fall
// back to their defaults on anything else, which would drop the whole palette.
// Round-tripping through a canvas `fillStyle` does not help: CSS Color 4 says a
// colour serializes in its own space, so Chrome hands `oklch(…)` straight back.
// So the conversion happens here, and is unit-tested rather than trusted.

/** `oklch(L C H)` or `oklch(L C H / A)`, with optional percent on L and A. */
const OKLCH = /^oklch\(\s*([\d.]+)(%?)\s+([\d.]+)(%?)\s+([\d.]+)(?:deg)?\s*(?:\/\s*([\d.]+)(%?)\s*)?\)$/i;

/** Space-separated channels as the tokens store them: "0.215 0.026 305". */
const CHANNELS = /^([\d.]+)\s+([\d.]+)\s+([\d.]+)$/;

/** An opaque colour plus its opacity, kept apart because Cytoscape does not read
 *  alpha out of a colour string — it has separate `*-opacity` properties and
 *  would render an `rgba(…, 0.1)` border at full strength. */
export interface RenderColor {
  color: string;
  opacity: number;
}

/** Converts a Dusk colour token into a renderer-ready colour + opacity pair.
 *
 *  Accepts either the bare channel triple the palette tokens hold or a full
 *  `oklch()` colour (the tokens that carry their own alpha). `alpha` applies to
 *  the channel form; a full colour's own alpha wins over it. Anything else —
 *  including a colour already in a form the renderer understands — is passed
 *  through unchanged, so callers can hand it tokens blind. */
export function oklchToRenderColor(value: string, alpha = 1): RenderColor {
  const raw = value.trim();

  const channels = CHANNELS.exec(raw);
  if (channels) {
    return { color: toRgb(Number(channels[1]), Number(channels[2]), Number(channels[3])), opacity: clamp(alpha) };
  }

  const parsed = OKLCH.exec(raw);
  if (parsed) {
    const l = pct(parsed[1], parsed[2], 1);
    const c = pct(parsed[3], parsed[4], 0.4); // 100% chroma is 0.4 in CSS Color 4
    const h = Number(parsed[5]);
    const a = parsed[6] === undefined ? alpha : pct(parsed[6], parsed[7], 1);
    return { color: toRgb(l, c, h), opacity: clamp(a) };
  }

  return { color: raw, opacity: clamp(alpha) };
}

function pct(value: string, percent: string, full: number): number {
  const n = Number(value);
  return percent === "%" ? (n / 100) * full : n;
}

/** OKLCH → OKLab → linear sRGB → gamma-encoded sRGB (CSS Color 4 matrices). */
function toRgb(l: number, c: number, h: number): string {
  const radians = (h * Math.PI) / 180;
  const a = c * Math.cos(radians);
  const b = c * Math.sin(radians);

  const long = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const medium = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const short = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;

  const red = 4.0767416621 * long - 3.3077115913 * medium + 0.2309699292 * short;
  const green = -1.2684380046 * long + 2.6097574011 * medium - 0.3413193965 * short;
  const blue = -0.0041960863 * long - 0.7034186147 * medium + 1.707614701 * short;

  return `rgb(${channel(red)}, ${channel(green)}, ${channel(blue)})`;
}

function clamp(alpha: number): number {
  return Math.round(Math.min(1, Math.max(0, alpha)) * 1000) / 1000;
}

/** Linear sRGB → 0–255, gamma-encoded and clamped (out-of-gamut colours clip). */
function channel(linear: number): number {
  const encoded =
    linear <= 0.0031308 ? 12.92 * linear : 1.055 * Math.pow(Math.max(linear, 0), 1 / 2.4) - 0.055;
  return Math.round(Math.min(1, Math.max(0, encoded)) * 255);
}

