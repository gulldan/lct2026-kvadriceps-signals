import { Check, LogOut } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";

import { useAuth } from "@/features/auth/AuthContext";
import { ROLE_META, useRole } from "@/features/auth/RoleContext";
import type { Role } from "@/shared/types";
import { toast } from "@/shared/ui/Sonner";
import { Avatar, AvatarFallback } from "@/shared/ui/Avatar";
import { Button } from "@/shared/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/DropdownMenu";

function initialsOf(username: string): string {
  return username.slice(0, 2).toUpperCase();
}

export function RoleSwitcher() {
  const { t } = useTranslation("auth");
  const { auth, logout } = useAuth();
  const { role, available, setRole } = useRole();
  const navigate = useNavigate();
  const meta = ROLE_META[role];
  const username = auth?.user.username ?? "—";
  // Видимые инициалы входят в имя кнопки: голосовой ввод называет то, что видит на экране.
  const initials = initialsOf(username);

  const handleSelect = (next: Role) => {
    if (next === role) return;
    setRole(next);
    navigate(ROLE_META[next].home);
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          className="shrink-0"
          aria-label={`${initials}, ${username}, ${meta.label}`}
        >
          <Avatar size="sm" aria-hidden>
            <AvatarFallback>{initials}</AvatarFallback>
          </Avatar>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-56" align="end">
        <DropdownMenuLabel className="truncate">
          {username}
          <span className="block text-xs font-normal text-muted-foreground">{meta.label}</span>
        </DropdownMenuLabel>
        {available.length > 1 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuLabel>{t("switcher.viewMode")}</DropdownMenuLabel>
            {available.map((r) => (
              <DropdownMenuItem key={r} onClick={() => handleSelect(r)}>
                <span className="flex-1">{ROLE_META[r].label}</span>
                {r === role && <Check className="size-4" aria-hidden />}
              </DropdownMenuItem>
            ))}
          </>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={() => {
            void logout().catch(() => toast.error(t("switcher.logoutFailed")));
          }}
        >
          <LogOut className="size-4" aria-hidden />
          <span className="flex-1">{t("switcher.logout")}</span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
