import { Link } from "react-router-dom"
import { ChevronsUpDown, LogOut, UserRound } from "lucide-react"
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

interface UserMenuProps {
  user: User
  onSignOut: () => void
}

export function UserMenu({ user, onSignOut }: UserMenuProps) {
  const { choice } = useTheme()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className="h-auto w-full justify-between gap-3 rounded-2xl border bg-muted px-3 py-2.5 hover:bg-accent"
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
          <ChevronsUpDown
            aria-hidden="true"
            strokeWidth={1.6}
            className="size-4 shrink-0 text-muted-foreground"
          />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuLabel>{user.displayName || user.username}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to="/account">
            <UserRound aria-hidden="true" className="size-4" />
            Account
          </Link>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuLabel className="text-xs text-muted-foreground">Theme</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={choice} onValueChange={(v) => setTheme(parseChoice(v))}>
          <DropdownMenuRadioItem value="light">Light</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark">Dark</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system">System</DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={onSignOut}>
          <LogOut aria-hidden="true" className="size-4" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
