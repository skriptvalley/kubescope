import { describe, expect, it } from "vitest";

import { readBasePath, routerBasename, withBase } from "./base";

function docWithBase(href?: string): Document {
  const doc = document.implementation.createHTMLDocument("t");
  if (href !== undefined) {
    const el = doc.createElement("base");
    el.setAttribute("href", href);
    doc.head.appendChild(el);
  }
  return doc;
}

describe("readBasePath (ADR-0012)", () => {
  it("is the root when the shell has no <base>", () => {
    expect(readBasePath(docWithBase())).toBe("");
  });

  it("is the root for <base href='/'>", () => {
    expect(readBasePath(docWithBase("/"))).toBe("");
  });

  it("reads a sub-path mount without the trailing slash", () => {
    expect(readBasePath(docWithBase("/kubescope/"))).toBe("/kubescope");
    expect(readBasePath(docWithBase("/tools/kubescope/"))).toBe("/tools/kubescope");
  });

  it("uses the path of an absolute base URL", () => {
    expect(readBasePath(docWithBase("https://projects.example.dev/kubescope/"))).toBe("/kubescope");
  });
});

describe("withBase", () => {
  it("prefixes root-absolute API paths with the mount", () => {
    expect(withBase("/api/v1/nodes", "/kubescope")).toBe("/kubescope/api/v1/nodes");
    expect(withBase("/api/v1/stream/pods/ns/p/logs?follow=false", "/kubescope")).toBe(
      "/kubescope/api/v1/stream/pods/ns/p/logs?follow=false",
    );
  });

  it("leaves paths untouched at the root", () => {
    expect(withBase("/api/v1/nodes", "")).toBe("/api/v1/nodes");
  });

  it("does not touch absolute or protocol-relative URLs", () => {
    expect(withBase("https://example.dev/x", "/kubescope")).toBe("https://example.dev/x");
    expect(withBase("//example.dev/x", "/kubescope")).toBe("//example.dev/x");
  });
});

describe("routerBasename", () => {
  it("uses the mount when the page was loaded under it", () => {
    expect(routerBasename("/kubescope/", "/kubescope")).toBe("/kubescope");
    expect(routerBasename("/kubescope", "/kubescope")).toBe("/kubescope");
    expect(routerBasename("/kubescope/resources/core/v1/pods", "/kubescope")).toBe("/kubescope");
  });

  it("falls back to / when reached without the prefix (e.g. a port-forward)", () => {
    expect(routerBasename("/", "/kubescope")).toBe("/");
    expect(routerBasename("/overview", "/kubescope")).toBe("/");
    expect(routerBasename("/kubescopex/overview", "/kubescope")).toBe("/");
  });

  it("is / at the root", () => {
    expect(routerBasename("/overview", "")).toBe("/");
  });
});
