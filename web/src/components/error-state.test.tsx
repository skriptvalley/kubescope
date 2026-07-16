import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { ApiError } from "@/lib/api";

describe("EmptyState", () => {
  it("renders the message", () => {
    render(<EmptyState message="No services found." />);
    expect(screen.getByTestId("empty-state")).toHaveTextContent("No services found.");
  });
});

describe("ErrorState", () => {
  it("renders a friendly title and detail from an ApiError", () => {
    render(<ErrorState error={new ApiError("secret gone", "not_found", 404)} />);
    expect(screen.getByTestId("error-state")).toHaveTextContent("Not found");
    expect(screen.getByTestId("error-state")).toHaveTextContent("secret gone (not_found)");
  });

  it("shows a retry action that calls onRetry", () => {
    const onRetry = vi.fn();
    render(<ErrorState error={new Error("boom")} onRetry={onRetry} title="Failed to load" />);
    fireEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("omits the retry button when no handler is given", () => {
    render(<ErrorState error={new Error("boom")} />);
    expect(screen.queryByRole("button", { name: /retry/i })).toBeNull();
  });

  it("surfaces ApiError guidance when present", () => {
    render(
      <ErrorState error={new ApiError("unreachable", "cluster_unreachable", 502, "check your VPN")} />,
    );
    expect(screen.getByTestId("error-state")).toHaveTextContent("check your VPN");
  });

  it("renders a Learn more link when the ApiError carries a docURL", () => {
    render(
      <ErrorState
        error={new ApiError("bad cert", "tls_cert", 502, "fix the CA", "https://docs.example/adr-0004")}
      />,
    );
    const link = screen.getByRole("link", { name: /learn more/i });
    expect(link).toHaveAttribute("href", "https://docs.example/adr-0004");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noreferrer");
  });

  it("omits the Learn more link when no docURL is present", () => {
    render(<ErrorState error={new ApiError("down", "cluster_unreachable", 502)} />);
    expect(screen.queryByRole("link", { name: /learn more/i })).toBeNull();
  });
});
