export function ErrorBanner({
  message,
  onRetry,
}: {
  message: string;
  onRetry?: () => void;
}) {
  return (
    <div
      role="alert"
      className="flex items-center justify-between gap-3 rounded-md border border-red-800 bg-red-950/60 px-3 py-2 text-sm text-red-200"
    >
      <span>
        <strong className="font-semibold">Error:</strong> {message}
      </span>
      {onRetry ? (
        <button
          type="button"
          onClick={onRetry}
          className="shrink-0 rounded border border-red-700 px-2 py-1 text-xs font-medium hover:bg-red-900"
        >
          Retry
        </button>
      ) : null}
    </div>
  );
}
