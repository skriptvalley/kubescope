import * as React from "react";

import { cn } from "@/lib/utils";

/** Minimal text input matching the shadcn/ui field styling used across the app. */
const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, type, ...props }, ref) => (
    <input
      ref={ref}
      type={type}
      className={cn(
        // Dusk field: flat, focus draws the ring colour + a soft 3px halo.
        "flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm transition-[color,box-shadow,border-color] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  ),
);
Input.displayName = "Input";

export { Input };
