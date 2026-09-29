import { createContext, useContext, type ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

type FilterBarSize = "default" | "sm";

// Высоту контролов задаёт ряд, а не каждый контрол: иначе поиск, селект и кнопка
// рядом расходятся на несколько пикселей.
const FilterBarContext = createContext<FilterBarSize | null>(null);

export function useFilterBarSize() {
  return useContext(FilterBarContext);
}

export function FilterBar({
  size = "default",
  className,
  ...props
}: ComponentProps<"div"> & { size?: FilterBarSize }) {
  return (
    <FilterBarContext value={size}>
      <div
        data-slot="filter-bar"
        data-size={size}
        className={cn("flex min-w-0 flex-wrap items-end gap-2", className)}
        {...props}
      />
    </FilterBarContext>
  );
}
