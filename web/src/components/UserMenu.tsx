import { CaretUpDown, SignOut, User as UserIcon } from "@phosphor-icons/react"
import { Link } from "react-router-dom"
import { RoleBadge } from "@/components/RoleBadge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import type { User } from "@/lib/permissions"
import { parseChoice, setTheme, useTheme } from "@/lib/theme"
import { languageNames, languages, parseLang, setLang, t, useLang } from "@/i18n"

interface UserMenuProps {
  user: User
  onSignOut: () => void
}

export function UserMenu({ user, onSignOut }: UserMenuProps) {
  const { choice } = useTheme()
  const lang = useLang()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className="h-auto w-full justify-between gap-3 rounded-2xl border bg-muted px-3 py-3 hover:bg-accent"
        >
          <span
            aria-hidden="true"
            className="grid size-8 shrink-0 place-items-center rounded-full bg-primary/15 text-sm font-medium text-brand uppercase"
          >
            {user.username.slice(0, 1)}
          </span>
          <span className="flex min-w-0 flex-1 flex-col items-start gap-1">
            <span className="max-w-full truncate">{user.username}</span>
            <RoleBadge role={user.role} />
          </span>
          <CaretUpDown aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuLabel>{user.displayName || user.username}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to="/account">
            <UserIcon aria-hidden="true" className="size-4" />
            {t("userMenu.account")}
          </Link>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuLabel className="text-xs text-muted-foreground">
          {t("userMenu.theme")}
        </DropdownMenuLabel>
        <DropdownMenuRadioGroup value={choice} onValueChange={(v) => setTheme(parseChoice(v))}>
          <DropdownMenuRadioItem value="light">{t("userMenu.light")}</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark">{t("userMenu.dark")}</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system">{t("userMenu.system")}</DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuLabel className="text-xs text-muted-foreground">
          {t("userMenu.language")}
        </DropdownMenuLabel>
        <DropdownMenuRadioGroup value={lang} onValueChange={(v) => setLang(parseLang(v))}>
          {languages.map((l) => (
            <DropdownMenuRadioItem key={l} value={l} lang={l}>
              {languageNames[l]}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={onSignOut}>
          <SignOut aria-hidden="true" className="size-4" />
          {t("userMenu.signOut")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
