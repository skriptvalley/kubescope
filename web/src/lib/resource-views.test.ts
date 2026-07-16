import { describe, expect, it } from "vitest";

import { detailKind, isSecret } from "@/lib/resource-views";

describe("detailKind", () => {
  it("maps core config/networking/storage kinds to typed views", () => {
    expect(detailKind({ group: "core", resource: "configmaps" })).toBe("configmap");
    expect(detailKind({ group: "core", resource: "secrets" })).toBe("secret");
    expect(detailKind({ group: "core", resource: "services" })).toBe("service");
    expect(detailKind({ group: "core", resource: "serviceaccounts" })).toBe("serviceaccount");
    expect(detailKind({ group: "core", resource: "persistentvolumeclaims" })).toBe(
      "persistentvolumeclaim",
    );
    expect(detailKind({ group: "core", resource: "persistentvolumes" })).toBe("persistentvolume");
    expect(detailKind({ group: "networking.k8s.io", resource: "ingresses" })).toBe("ingress");
    expect(detailKind({ group: "storage.k8s.io", resource: "storageclasses" })).toBe(
      "storageclass",
    );
  });

  it("separates namespaced Role from cluster-scoped ClusterRole", () => {
    expect(detailKind({ group: "rbac.authorization.k8s.io", resource: "roles" })).toBe("role");
    expect(detailKind({ group: "rbac.authorization.k8s.io", resource: "clusterroles" })).toBe(
      "clusterrole",
    );
    expect(detailKind({ group: "rbac.authorization.k8s.io", resource: "rolebindings" })).toBe(
      "rolebinding",
    );
    expect(
      detailKind({ group: "rbac.authorization.k8s.io", resource: "clusterrolebindings" }),
    ).toBe("clusterrolebinding");
  });

  it("returns undefined for unregistered kinds (CRDs, pods)", () => {
    expect(detailKind({ group: "core", resource: "pods" })).toBeUndefined();
    expect(detailKind({ group: "example.com", resource: "widgets" })).toBeUndefined();
  });
});

describe("isSecret", () => {
  it("is true only for the core Secret kind", () => {
    expect(isSecret({ group: "core", resource: "secrets" })).toBe(true);
    expect(isSecret({ group: "core", resource: "configmaps" })).toBe(false);
    expect(isSecret({ group: "external", resource: "secrets" })).toBe(false);
  });
});
