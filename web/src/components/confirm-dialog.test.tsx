import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ApiError } from "@/lib/api";

import { ConfirmDialog } from "./confirm-dialog";

describe("ConfirmDialog", () => {
  it("renders nothing when closed", () => {
    const { container } = render(
      <ConfirmDialog open={false} title="X" onConfirm={() => {}} onCancel={() => {}} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("confirms immediately when there is no typed-name gate", () => {
    const onConfirm = vi.fn();
    render(<ConfirmDialog open title="Restart web?" onConfirm={onConfirm} onCancel={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(onConfirm).toHaveBeenCalledOnce();
  });

  it("gates the destructive confirm on typing the exact name", () => {
    const onConfirm = vi.fn();
    render(
      <ConfirmDialog
        open
        destructive
        title="Delete pod web-1?"
        confirmText="web-1"
        confirmLabel="Delete"
        onConfirm={onConfirm}
        onCancel={() => {}}
      />,
    );

    const confirm = screen.getByRole("button", { name: "Delete" });
    expect(confirm).toBeDisabled();

    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "web-2" } });
    expect(confirm).toBeDisabled();

    fireEvent.change(input, { target: { value: "web-1" } });
    expect(confirm).toBeEnabled();

    fireEvent.click(confirm);
    expect(onConfirm).toHaveBeenCalledOnce();
  });

  it("surfaces the action error in-dialog", () => {
    render(
      <ConfirmDialog
        open
        title="Delete?"
        error={new ApiError("forbidden: cannot delete", "forbidden", 403)}
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );
    expect(screen.getByTestId("confirm-error")).toHaveTextContent("forbidden: cannot delete (forbidden)");
  });

  it("cancels via the Cancel button", () => {
    const onCancel = vi.fn();
    render(<ConfirmDialog open title="X" onConfirm={() => {}} onCancel={onCancel} />);
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it("renders the cascade warning slot when provided (namespace delete)", () => {
    render(
      <ConfirmDialog
        open
        destructive
        title="Delete Namespace payments?"
        cascade={<>Everything in payments is deleted with it — all pods, services, config maps and secrets.</>}
        confirmText="payments"
        confirmLabel="Delete namespace"
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );
    expect(screen.getByText(/everything in payments is deleted with it/i)).toBeInTheDocument();
    // The typed-name gate still guards the destructive action.
    expect(screen.getByRole("button", { name: "Delete namespace" })).toBeDisabled();
  });

  it("omits the cascade box when no cascade is passed", () => {
    render(<ConfirmDialog open title="Delete Pod web-1?" confirmText="web-1" onConfirm={() => {}} onCancel={() => {}} />);
    expect(screen.queryByText(/deleted with it/i)).toBeNull();
  });
});
