import { useMemo, useState } from "react";
import { JournalHighlights } from "./JournalHighlights";
import { classifyAccount, formatCurrency, parseHledgerJournal } from "./parser";
import { SankeyDiagram } from "./SankeyDiagram";

export function JournalPreviewPane({
  content,
  filePath: _filePath,
  onSwitchToEdit,
}: {
  content: string;
  filePath: string;
  onSwitchToEdit?: () => void;
}) {
  const [activeTab, setActiveTab] = useState<"transactions" | "accounts">(
    "transactions",
  );
  const [filterQuery, setFilterQuery] = useState("");

  const parsed = useMemo(() => {
    return parseHledgerJournal(content);
  }, [content]);

  const { transactions, accounts, highlights, sankeyNodes, sankeyLinks } =
    parsed;
  const primaryCurrency = highlights.currency;

  const filteredTransactions = useMemo(() => {
    const q = filterQuery.toLowerCase().trim();
    if (!q) return transactions;
    return transactions.filter(
      (tx) =>
        tx.description.toLowerCase().includes(q) ||
        tx.date.includes(q) ||
        tx.postings.some((p) => p.account.toLowerCase().includes(q)),
    );
  }, [transactions, filterQuery]);

  const sortedAccounts = useMemo(() => {
    return Array.from(accounts.values()).sort((a, b) => {
      if (a.rootType !== b.rootType)
        return a.rootType.localeCompare(b.rootType);
      return a.name.localeCompare(b.name);
    });
  }, [accounts]);

  if (transactions.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center p-8 text-center">
        <div className="rounded-full bg-base-200 p-4">
          <svg
            aria-hidden="true"
            className="h-8 w-8 text-base-content/60"
            fill="none"
            stroke="currentColor"
            strokeWidth={1.5}
            viewBox="0 0 24 24"
          >
            <path
              d="M12 6.042A8.967 8.967 0 006 3.75c-1.052 0-2.062.18-3 .512v14.25A8.987 8.987 0 016 18c2.305 0 4.408.867 6 2.292m0-14.25a8.966 8.966 0 016-2.292c1.052 0 2.062.18 3 .512v14.25A8.987 8.987 0 0018 18a8.967 8.967 0 00-6 2.292m0-14.25v14.25"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </div>
        <h3 className="mt-4 text-base font-semibold">
          No journal transactions detected
        </h3>
        <p className="mt-1 max-w-sm text-xs text-base-content/60">
          This file is empty or does not contain recognizable hledger
          transactions yet.
        </p>
        {onSwitchToEdit && (
          <button
            className="btn btn-primary btn-sm mt-4"
            onClick={onSwitchToEdit}
            type="button"
          >
            Switch to Code Editor
          </button>
        )}
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col overflow-y-auto p-4 md:p-6 gap-6">
      {/* 1. Sankey Diagram at Top */}
      <section aria-label="Sankey Diagram Flow">
        <SankeyDiagram
          currency={primaryCurrency}
          links={sankeyLinks}
          nodes={sankeyNodes}
        />
      </section>

      {/* 2. Key Highlights */}
      <section aria-label="Journal Highlights">
        <JournalHighlights highlights={highlights} />
      </section>

      {/* 3. Detailed Data Breakdown (Transactions / Accounts) */}
      <section
        aria-label="Ledger Data Breakdown"
        className="flex flex-col gap-3"
      >
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-base-content/10 pb-2">
          <div className="join">
            <button
              className={`btn btn-sm join-item ${activeTab === "transactions" ? "btn-primary" : "btn-ghost"}`}
              onClick={() => setActiveTab("transactions")}
              type="button"
            >
              Transactions ({transactions.length})
            </button>
            <button
              className={`btn btn-sm join-item ${activeTab === "accounts" ? "btn-primary" : "btn-ghost"}`}
              onClick={() => setActiveTab("accounts")}
              type="button"
            >
              Account Balances ({accounts.size})
            </button>
          </div>

          {activeTab === "transactions" && (
            <div className="w-full sm:w-64">
              <input
                className="input input-sm input-bordered w-full text-xs"
                onChange={(e) => setFilterQuery(e.target.value)}
                placeholder="Search transactions or accounts..."
                type="search"
                value={filterQuery}
              />
            </div>
          )}
        </div>

        {activeTab === "transactions" ? (
          <div className="flex flex-col gap-2.5">
            {filteredTransactions.map((tx) => (
              <div
                key={tx.id}
                className="rounded-lg border border-base-content/10 bg-base-100 p-3 shadow-2xs hover:border-base-content/20 transition-colors"
              >
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-xs font-semibold text-base-content/70">
                      {tx.date}
                    </span>
                    {tx.status && (
                      <span className="badge badge-xs badge-success">
                        {tx.status}
                      </span>
                    )}
                    <span className="text-xs font-medium text-base-content">
                      {tx.description}
                    </span>
                  </div>
                  <span className="text-[10px] font-mono text-base-content/40">
                    Line {tx.lineNumber}
                  </span>
                </div>

                <div className="mt-2 divide-y divide-base-content/5 rounded bg-base-200/40 font-mono text-[11px]">
                  {tx.postings.map((p) => {
                    const accType = classifyAccount(p.account);
                    const isCredit = p.amount < 0;
                    return (
                      <div
                        key={`${tx.id}-${p.account}-${p.amount}`}
                        className="flex items-center justify-between px-2.5 py-1"
                      >
                        <span
                          className={`truncate ${
                            accType === "income"
                              ? "text-emerald-600 dark:text-emerald-400 font-medium"
                              : accType === "expenses"
                                ? "text-rose-600 dark:text-rose-400"
                                : "text-sky-600 dark:text-sky-400"
                          }`}
                        >
                          {p.account}
                        </span>
                        <span
                          className={`font-semibold shrink-0 ml-4 ${
                            isCredit
                              ? "text-emerald-600 dark:text-emerald-400"
                              : "text-base-content/80"
                          }`}
                        >
                          {formatCurrency(p.amount, p.currency)}
                        </span>
                      </div>
                    );
                  })}
                </div>
              </div>
            ))}

            {filteredTransactions.length === 0 && (
              <div className="p-4 text-center text-xs text-base-content/50">
                No transactions match "{filterQuery}".
              </div>
            )}
          </div>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-base-content/10 bg-base-100 shadow-2xs">
            <table className="table table-sm text-xs">
              <thead>
                <tr className="border-b border-base-content/10 bg-base-200/50">
                  <th>Account</th>
                  <th>Type</th>
                  <th className="text-right">Inflow / Credit</th>
                  <th className="text-right">Outflow / Debit</th>
                  <th className="text-right">Net Balance</th>
                </tr>
              </thead>
              <tbody>
                {sortedAccounts.map((acc) => (
                  <tr key={acc.name} className="hover:bg-base-200/30">
                    <td className="font-mono font-medium">{acc.name}</td>
                    <td>
                      <span
                        className={`badge badge-xs uppercase ${
                          acc.rootType === "income"
                            ? "badge-success"
                            : acc.rootType === "expenses"
                              ? "badge-error"
                              : acc.rootType === "assets"
                                ? "badge-info"
                                : "badge-neutral"
                        }`}
                      >
                        {acc.rootType}
                      </span>
                    </td>
                    <td className="text-right font-mono text-emerald-600 dark:text-emerald-400">
                      {acc.inflow > 0
                        ? formatCurrency(acc.inflow, acc.currency)
                        : "—"}
                    </td>
                    <td className="text-right font-mono text-rose-600 dark:text-rose-400">
                      {acc.outflow > 0
                        ? formatCurrency(acc.outflow, acc.currency)
                        : "—"}
                    </td>
                    <td className="text-right font-mono font-bold">
                      {formatCurrency(acc.net, acc.currency)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
