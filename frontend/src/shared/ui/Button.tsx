import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";
import { useFilterBarSize } from "@/shared/ui/FilterBar";

const buttonVariants = cva(
  "inline-flex max-w-full cursor-pointer items-center justify-center gap-2 rounded-lg text-sm font-medium wrap-anywhere whitespace-normal transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none active:translate-y-px disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*=size-])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/85 active:bg-primary/75",
        destructive: "bg-destructive text-destructive-foreground hover:bg-destructive/90",
        outline:
          "border border-input bg-background text-foreground hover:border-foreground/30 aria-expanded:border-brand-border aria-expanded:bg-brand-wash aria-expanded:text-accent-foreground aria-pressed:border-brand-border aria-pressed:bg-brand-wash aria-pressed:text-accent-foreground",
        secondary: "bg-secondary text-secondary-foreground hover:bg-secondary/70 active:bg-border",
        accent: "bg-brand-wash text-accent-foreground hover:bg-brand-border active:bg-brand-border",
        ghost: "text-foreground hover:bg-secondary active:bg-border",
        link: "text-accent-foreground underline underline-offset-4 hover:decoration-2",
      },
      size: {
        default: "min-h-control px-4 py-1.5",
        sm: "min-h-control-sm px-3 py-1 text-xs",
        lg: "min-h-control-lg px-6 py-2.5",
        icon: "size-control",
        "icon-sm": "size-control-sm",
        inline: "h-auto min-h-6 gap-1.5 p-0 whitespace-normal",
        content: "h-auto justify-start font-normal whitespace-normal",
        row: "h-auto min-h-control w-full justify-start rounded-none px-2 py-3 text-left font-normal whitespace-normal",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

type ButtonSize = NonNullable<VariantProps<typeof buttonVariants>["size"]>;

const FILTER_BAR_SIZES: Record<"default" | "sm", Partial<Record<ButtonSize, ButtonSize>>> = {
  default: { sm: "default", lg: "default", "icon-sm": "icon" },
  sm: { default: "sm", lg: "sm", icon: "icon-sm" },
};

export function Button({
  className,
  variant = "default",
  size: requested = "default",
  asChild = false,
  type,
  ...props
}: ComponentProps<"button"> & VariantProps<typeof buttonVariants> & { asChild?: boolean }) {
  const bar = useFilterBarSize();
  const size = (bar && requested && FILTER_BAR_SIZES[bar][requested]) || requested;
  const Comp = asChild ? Slot : "button";
  return (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      type={type ?? (asChild ? undefined : "button")}
      className={cn(buttonVariants({ variant, size }), className)}
      {...props}
    />
  );
}

export { buttonVariants };
