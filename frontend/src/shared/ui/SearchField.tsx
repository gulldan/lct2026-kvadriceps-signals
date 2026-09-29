import { Search } from "lucide-react";
import type { ComponentProps } from "react";

import { Input } from "@/shared/ui/Input";

interface SearchFieldProps extends Omit<
  ComponentProps<typeof Input>,
  "value" | "onChange" | "placeholder" | "aria-label"
> {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  ariaLabel: string;
  className?: string;
}

export function SearchField({
  value,
  onChange,
  placeholder,
  ariaLabel,
  className = "",
  ...props
}: SearchFieldProps) {
  return (
    <div className={`relative ${className}`}>
      <Search
        className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
        aria-hidden
      />
      <Input
        {...props}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="pl-9"
        aria-label={ariaLabel}
      />
    </div>
  );
}
