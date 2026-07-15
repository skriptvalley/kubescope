import { AlertCircle } from "lucide-react";

import { StatusBadge } from "@/components/status-badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { useEvents } from "@/hooks/use-events";
import { formatAge } from "@/lib/age";
import { ApiError, type EventSummary } from "@/lib/api";
import { eventTypeTone } from "@/lib/workload-status";

/** The shared events panel embedded on every workload detail view. Events are
 *  filtered to this object server-side (involvedObject) and returned newest
 *  first; Warning events are visually distinct from Normal. */
export function EventsPanel({
  kind,
  namespace,
  name,
}: {
  kind: string;
  namespace?: string;
  name: string;
}) {
  const { data, isPending, isError, error } = useEvents({ kind, namespace, name });

  return (
    <section className="space-y-2" aria-label="Events">
      <h3 className="text-sm font-semibold">Events</h3>
      {isPending ? (
        <Skeleton className="h-16 w-full" data-testid="events-loading" />
      ) : isError ? (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Failed to load events</AlertTitle>
          <AlertDescription>
            {error instanceof ApiError ? `${error.message} (${error.code})` : error.message}
          </AlertDescription>
        </Alert>
      ) : data.length === 0 ? (
        <p className="text-sm text-muted-foreground">No events recorded for this object.</p>
      ) : (
        <ul className="divide-y rounded-md border" data-testid="events-list">
          {data.map((event, i) => (
            <EventRow key={`${event.reason}-${event.lastSeen ?? ""}-${i}`} event={event} />
          ))}
        </ul>
      )}
    </section>
  );
}

function EventRow({ event }: { event: EventSummary }) {
  return (
    <li className="flex items-start gap-3 px-3 py-2 text-sm">
      <StatusBadge tone={eventTypeTone(event.type)}>{event.type}</StatusBadge>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-medium">{event.reason}</span>
          {event.count > 1 && (
            <span className="text-xs text-muted-foreground">×{event.count}</span>
          )}
        </div>
        <p className="break-words text-muted-foreground">{event.message}</p>
      </div>
      <span className="shrink-0 text-xs text-muted-foreground" title={event.lastSeen}>
        {formatAge(event.lastSeen)}
      </span>
    </li>
  );
}
