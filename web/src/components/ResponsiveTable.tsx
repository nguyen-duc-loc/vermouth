import type { ReactNode } from 'react'

export type ResponsiveTableProps<TRow> = {
  rows: readonly TRow[]
  getRowKey: (row: TRow) => string
  renderTable: (rows: readonly TRow[]) => ReactNode
  renderCard: (row: TRow) => ReactNode
  emptyState: ReactNode
}

/** Requires callers to preserve table semantics and design a separate phone reading order. */
export function ResponsiveTable<TRow>({
  rows,
  getRowKey,
  renderTable,
  renderCard,
  emptyState,
}: ResponsiveTableProps<TRow>) {
  if (rows.length === 0) {
    return <div data-responsive-table-state="empty">{emptyState}</div>
  }

  return (
    <>
      <div className="grid gap-3 md:hidden">
        {rows.map((row) => (
          <div key={getRowKey(row)}>{renderCard(row)}</div>
        ))}
      </div>
      <div className="hidden md:block">{renderTable(rows)}</div>
    </>
  )
}
