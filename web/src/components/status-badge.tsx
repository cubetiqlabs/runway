import { HugeiconsIcon } from "@hugeicons/react"
import {
  CheckmarkCircle02Icon,
  Clock01Icon,
  CancelCircleIcon,
  Alert02Icon,
  Loading03Icon,
  PauseCircleIcon,
  Shield01Icon,
} from "@hugeicons/core-free-icons"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

interface StatusStyle {
  label: string
  className: string
  icon: typeof CheckmarkCircle02Icon
  spin?: boolean
}

const STATUS_MAP: Record<string, StatusStyle> = {
  online: {
    label: "Online",
    className: "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
    icon: CheckmarkCircle02Icon,
  },
  busy: {
    label: "Busy",
    className: "border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400",
    icon: Loading03Icon,
    spin: true,
  },
  provisioning: {
    label: "Provisioning",
    className: "border-sky-500/30 bg-sky-500/10 text-sky-600 dark:text-sky-400",
    icon: Loading03Icon,
    spin: true,
  },
  draining: {
    label: "Draining",
    className: "border-orange-500/30 bg-orange-500/10 text-orange-600 dark:text-orange-400",
    icon: PauseCircleIcon,
  },
  offline: {
    label: "Offline",
    className: "border-border bg-muted text-muted-foreground",
    icon: CancelCircleIcon,
  },
  terminated: {
    label: "Terminated",
    className: "border-border bg-muted text-muted-foreground",
    icon: CancelCircleIcon,
  },
  queued: {
    label: "Queued",
    className: "border-sky-500/30 bg-sky-500/10 text-sky-600 dark:text-sky-400",
    icon: Clock01Icon,
  },
  awaiting_approval: {
    label: "Awaiting Approval",
    className: "border-amber-500/40 bg-amber-500/15 text-amber-700 dark:text-amber-300",
    icon: Alert02Icon,
  },
  in_progress: {
    label: "In Progress",
    className: "border-primary/40 bg-primary/10 text-primary",
    icon: Loading03Icon,
    spin: true,
  },
  completed: {
    label: "Completed",
    className: "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
    icon: CheckmarkCircle02Icon,
  },
  rejected: {
    label: "Rejected",
    className: "border-destructive/40 bg-destructive/10 text-destructive",
    icon: CancelCircleIcon,
  },
  cancelled: {
    label: "Cancelled",
    className: "border-border bg-muted text-muted-foreground",
    icon: CancelCircleIcon,
  },
  allow: {
    label: "Allow",
    className: "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
    icon: CheckmarkCircle02Icon,
  },
  approved: {
    label: "Approved",
    className: "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
    icon: CheckmarkCircle02Icon,
  },
  deny: {
    label: "Deny",
    className: "border-destructive/40 bg-destructive/10 text-destructive",
    icon: CancelCircleIcon,
  },
  require_approval: {
    label: "Require Approval",
    className: "border-amber-500/40 bg-amber-500/15 text-amber-700 dark:text-amber-300",
    icon: Shield01Icon,
  },
  processed: {
    label: "Processed",
    className: "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
    icon: CheckmarkCircle02Icon,
  },
  ignored: {
    label: "Ignored",
    className: "border-border bg-muted text-muted-foreground",
    icon: PauseCircleIcon,
  },
  failed: {
    label: "Failed",
    className: "border-destructive/40 bg-destructive/10 text-destructive",
    icon: Alert02Icon,
  },
}

export function StatusBadge({ status, className }: { status: string; className?: string }) {
  const cfg = STATUS_MAP[status] ?? {
    label: status || "Unknown",
    className: "border-border bg-muted text-muted-foreground",
    icon: Clock01Icon,
  }
  return (
    <Badge variant="outline" className={cn("gap-1 font-medium", cfg.className, className)}>
      <HugeiconsIcon
        icon={cfg.icon}
        strokeWidth={2}
        className={cn("size-3", cfg.spin && "animate-spin")}
      />
      <span>{cfg.label}</span>
    </Badge>
  )
}
