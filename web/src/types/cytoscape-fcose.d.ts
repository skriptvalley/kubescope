// cytoscape-fcose ships no TypeScript types (ADR-0011). It is a Cytoscape layout
// extension, so the only surface we use is the default export passed to
// cytoscape.use() — declared here rather than reaching for `any` at the call site.
declare module "cytoscape-fcose" {
  import type { Ext } from "cytoscape";
  const fcose: Ext;
  export default fcose;
}
