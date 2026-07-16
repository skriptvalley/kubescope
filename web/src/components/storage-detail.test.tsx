import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import {
  PersistentVolumeClaimDetail,
  PersistentVolumeDetail,
  StorageClassDetail,
} from "@/components/storage-detail";
import type { KubeObject } from "@/lib/api";

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

describe("PersistentVolumeClaimDetail", () => {
  it("shows a bound PVC linking to its PV", () => {
    const object = {
      spec: {
        volumeName: "pv-abc",
        storageClassName: "standard",
        accessModes: ["ReadWriteOnce"],
      },
      status: { phase: "Bound", capacity: { storage: "1Gi" } },
    } as KubeObject;
    renderWithRouter(<PersistentVolumeClaimDetail object={object} />);
    expect(screen.getByText("Bound")).toBeInTheDocument();
    expect(screen.getByText("1Gi")).toBeInTheDocument();
    expect(screen.getByText("RWO")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "pv-abc" })).toHaveAttribute(
      "href",
      "/resources/core/v1/persistentvolumes/pv-abc",
    );
  });

  it("renders a meaningful status for a pending, unbound PVC", () => {
    const object = { spec: {}, status: { phase: "Pending" } } as KubeObject;
    renderWithRouter(<PersistentVolumeClaimDetail object={object} />);
    expect(screen.getByText("Pending")).toBeInTheDocument();
    expect(screen.getByText("Not bound")).toBeInTheDocument();
  });
});

describe("PersistentVolumeDetail", () => {
  it("shows reclaim policy, phase and a linked claim reference", () => {
    const object = {
      spec: {
        capacity: { storage: "10Gi" },
        persistentVolumeReclaimPolicy: "Retain",
        claimRef: { namespace: "default", name: "data-0" },
      },
      status: { phase: "Bound" },
    } as KubeObject;
    renderWithRouter(<PersistentVolumeDetail object={object} />);
    expect(screen.getByText("Retain")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "default/data-0" })).toHaveAttribute(
      "href",
      "/resources/core/v1/persistentvolumeclaims/default/data-0",
    );
  });
});

describe("StorageClassDetail", () => {
  it("marks the default class", () => {
    const object = {
      provisioner: "rancher.io/local-path",
      metadata: {
        annotations: { "storageclass.kubernetes.io/is-default-class": "true" },
      },
    } as KubeObject;
    renderWithRouter(<StorageClassDetail object={object} />);
    expect(screen.getByText("rancher.io/local-path")).toBeInTheDocument();
    // The "Default" field shows a "Yes" badge for the default class.
    expect(screen.getByText("Yes")).toBeInTheDocument();
  });

  it("marks a non-default class as No", () => {
    const object = { provisioner: "kubernetes.io/aws-ebs" } as KubeObject;
    renderWithRouter(<StorageClassDetail object={object} />);
    expect(screen.getByText("No")).toBeInTheDocument();
  });
});
