// Sub-path support (ADR-0012). Behind a reverse proxy Kubescope may be served
// under a prefix such as /kubescope/: the server writes <base href="/kubescope/">
// into index.html (KUBESCOPE_BASE_PATH), the build's asset URLs are relative to
// it (Vite base "./"), and this module reads it back so the router basename and
// every API / SSE / WebSocket URL follow suit. With no <base> — the Vite dev
// server, unit tests — the app lives at the root and nothing changes.

/** The mount path from the document's `<base href>`: "" at the root, else e.g.
 *  "/kubescope" (never a trailing slash). */
export function readBasePath(doc: Document = document): string {
  const base = doc.querySelector<HTMLBaseElement>("base[href]");
  if (!base) return "";
  try {
    // base.href is already absolute in a live page; a detached document (no URL
    // to resolve against) yields the raw attribute, hence the placeholder origin.
    return new URL(base.href, "http://localhost/").pathname.replace(/\/+$/, "");
  } catch {
    return "";
  }
}

/** The mount path, read once at startup (the shell never changes it). */
export const basePath = readBasePath();

/** Prefixes a root-absolute app path ("/api/v1/…") with the mount path. Any
 *  other value (already absolute URL, relative path) is returned unchanged. */
export function withBase(path: string, base: string = basePath): string {
  return path.startsWith("/") && !path.startsWith("//") ? `${base}${path}` : path;
}

/** The router basename: the mount when the page was loaded under it, else "/".
 *  The server also answers unprefixed paths (e.g. a port-forward that bypasses
 *  the proxy), and a basename that matches nothing would render a blank page. */
export function routerBasename(pathname: string = location.pathname, base: string = basePath): string {
  if (base && (pathname === base || pathname.startsWith(`${base}/`))) return base;
  return "/";
}
