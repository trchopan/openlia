import type { JournalHighlightsData } from "./parser";
import { formatCurrency } from "./parser";

export function JournalHighlights({
  highlights,
}: {
  highlights: JournalHighlightsData;
}) {
  const {
    totalIncome,
    totalExpenses,
    netSavings,
    savingsRate,
    transactionCount,
    startDate,
    endDate,
    currency,
    topExpenseCategories,
  } = highlights;

  return (
    <div className="flex flex-col gap-3">
      {/* 4 Stat Cards Grid */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {/* Total Income */}
        <div className="rounded-xl border border-emerald-500/20 bg-emerald-500/5 p-3.5 shadow-xs">
          <div className="flex items-center justify-between text-xs font-medium text-emerald-600 dark:text-emerald-400">
            <span>TOTAL INCOME</span>
            <span className="text-base">↓</span>
          </div>
          <div className="mt-1 text-xl font-bold tracking-tight text-emerald-700 dark:text-emerald-300">
            {formatCurrency(totalIncome, currency)}
          </div>
          <div className="mt-1 text-[11px] text-base-content/60">
            Earned inflows & deposits
          </div>
        </div>

        {/* Total Expenses */}
        <div className="rounded-xl border border-rose-500/20 bg-rose-500/5 p-3.5 shadow-xs">
          <div className="flex items-center justify-between text-xs font-medium text-rose-600 dark:text-rose-400">
            <span>TOTAL EXPENSES</span>
            <span className="text-base">↑</span>
          </div>
          <div className="mt-1 text-xl font-bold tracking-tight text-rose-700 dark:text-rose-300">
            {formatCurrency(totalExpenses, currency)}
          </div>
          <div className="mt-1 text-[11px] text-base-content/60">
            Recorded outflows
          </div>
        </div>

        {/* Net Retained */}
        <div className="rounded-xl border border-violet-500/20 bg-violet-500/5 p-3.5 shadow-xs">
          <div className="flex items-center justify-between text-xs font-medium text-violet-600 dark:text-violet-400">
            <span>NET RETAINED</span>
            <span className="text-xs font-semibold">
              {savingsRate.toFixed(1)}%
            </span>
          </div>
          <div className="mt-1 text-xl font-bold tracking-tight text-violet-700 dark:text-violet-300">
            {netSavings >= 0 ? "+" : ""}
            {formatCurrency(netSavings, currency)}
          </div>
          <div className="mt-1 text-[11px] text-base-content/60">
            {netSavings >= 0 ? "Surplus saved / retained" : "Net deficit"}
          </div>
        </div>

        {/* Transactions & Period */}
        <div className="rounded-xl border border-sky-500/20 bg-sky-500/5 p-3.5 shadow-xs">
          <div className="flex items-center justify-between text-xs font-medium text-sky-600 dark:text-sky-400">
            <span>ACTIVITY</span>
            <span className="text-xs font-semibold">{currency}</span>
          </div>
          <div className="mt-1 text-xl font-bold tracking-tight text-sky-700 dark:text-sky-300">
            {transactionCount} <span className="text-sm font-normal">txs</span>
          </div>
          <div
            className="mt-1 truncate text-[11px] text-base-content/60"
            title={`${startDate || "?"} to ${endDate || "?"}`}
          >
            {startDate ? `${startDate} → ${endDate}` : "No dated entries"}
          </div>
        </div>
      </div>

      {/* Top Expense Breakdown Pill List */}
      {topExpenseCategories.length > 0 && (
        <div className="rounded-xl border border-base-content/10 bg-base-100 p-3 shadow-xs">
          <div className="mb-2 flex items-center justify-between text-xs font-semibold uppercase tracking-wider text-base-content/70">
            <span>Top Outflow Categories</span>
            <span className="text-[11px] font-normal text-base-content/50">
              Share of total expenses
            </span>
          </div>
          <div className="flex flex-wrap gap-2">
            {topExpenseCategories.map((cat) => (
              <div
                key={cat.name}
                className="flex items-center gap-1.5 rounded-lg border border-base-content/10 bg-base-200/60 px-2.5 py-1 text-xs"
              >
                <span className="font-medium text-base-content/80">
                  {cat.name.replace(/^expenses:/i, "")}
                </span>
                <span className="font-semibold text-rose-600 dark:text-rose-400">
                  {formatCurrency(cat.amount, currency)}
                </span>
                <span className="text-[10px] text-base-content/50">
                  ({cat.percentage.toFixed(1)}%)
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
