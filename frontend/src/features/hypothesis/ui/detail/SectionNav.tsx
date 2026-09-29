import { Button } from "@/shared/ui/Button";
import { useEffect, useState } from "react";

import { Card } from "@/shared/ui/Card";

export interface SectionLink {
  id: string;
  label: string;
  count?: number;
}

export function SectionNav({ title, sections }: { title: string; sections: SectionLink[] }) {
  const idKey = sections.map((s) => s.id).join("|");
  const [active, setActive] = useState(sections[0]?.id ?? "");

  useEffect(() => {
    const els = idKey
      .split("|")
      .map((id) => document.getElementById(id))
      .filter((el): el is HTMLElement => el !== null);
    if (els.length === 0) {
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        const first = entries
          .filter((e) => e.isIntersecting)
          .toSorted((x, y) => x.boundingClientRect.top - y.boundingClientRect.top)
          .at(0);
        if (first) {
          setActive(first.target.id);
        }
      },
      { rootMargin: "-15% 0px -70% 0px" },
    );
    for (const el of els) {
      observer.observe(el);
    }
    return () => observer.disconnect();
  }, [idKey]);

  const go = (id: string) => {
    document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" });
    setActive(id);
  };

  return (
    <Card asChild className="p-4">
      <nav aria-label={title}>
        <p className="kicker text-muted-foreground">{title}</p>
        <ul className="mt-2.5 space-y-0.5">
          {sections.map((s) => (
            <li key={s.id}>
              <Button
                variant={active === s.id ? "secondary" : "ghost"}
                size="sm"
                type="button"
                onClick={() => go(s.id)}
                aria-current={active === s.id ? "true" : undefined}
                className="w-full justify-between text-left"
              >
                <span className="truncate">{s.label}</span>
                {s.count !== undefined && s.count > 0 && (
                  <span className="rounded-full bg-muted px-1.5 font-mono text-caption text-muted-foreground tabular-nums">
                    {s.count}
                  </span>
                )}
              </Button>
            </li>
          ))}
        </ul>
      </nav>
    </Card>
  );
}
