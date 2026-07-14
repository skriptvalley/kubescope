// Builds the sidebar nav from the discovery payload — grouped by API group,
// entirely data-driven (no hardcoded resource list). Pure so it can be tested.

import { type APIResourceInfo, type Discovery, groupToken } from "@/lib/api";

export interface NavResource {
  /** Stable unique key: group/version/resource. */
  key: string;
  /** Display label (the Kind). */
  label: string;
  resource: string;
  namespaced: boolean;
  /** Client-side route to this resource's list page. */
  to: string;
}

export interface NavGroup {
  /** API group name, "" for core. */
  name: string;
  /** Display label, "core" for the empty group. */
  label: string;
  resources: NavResource[];
}

/** Resolves a resource (by URL-token group/version/resource) against discovery,
 *  so a page can learn its scope and kind before listing. */
export function findResource(
  discovery: Discovery | undefined,
  ref: { group: string; version: string; resource: string },
): APIResourceInfo | undefined {
  if (!discovery) return undefined;
  for (const group of discovery.groups) {
    for (const r of group.resources) {
      if (groupToken(r.group) === ref.group && r.version === ref.version && r.resource === ref.resource) {
        return r;
      }
    }
  }
  return undefined;
}

export function buildNav(discovery: Discovery | undefined): NavGroup[] {
  if (!discovery) return [];
  return discovery.groups.map((group) => ({
    name: group.name,
    label: group.name === "" ? "core" : group.name,
    resources: group.resources.map((r) => ({
      key: `${r.group}/${r.version}/${r.resource}`,
      label: r.kind,
      resource: r.resource,
      namespaced: r.namespaced,
      to: `/resources/${groupToken(r.group)}/${r.version}/${r.resource}`,
    })),
  }));
}
