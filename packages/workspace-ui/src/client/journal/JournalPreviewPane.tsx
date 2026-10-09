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
  const [selectedCurrency, setSelectedCurrency] = useState("all");
  const [sankeyCurrency, setSankeyCurrency] = useState<string | null>(null);

  const parsed = useMemo(() => {
    return parseHledgerJournal(content);
  }, [content]);

  const { transactions, accounts, highlightsByCurrency, sankeyByCurrency } =
    parsed;
  const currencies = parsed.currencies;
  const hasMultipleCurrencies = currencies.length > 1;
  const effectiveSelectedCurrency =
    selectedCurrency !== "all" && currencies.includes(selectedCurrency)
      ? selectedCurrency
      : "all";
  const activeCurrencies =
    effectiveSelectedCurrency === "all"
      ? currencies
      : [effectiveSelectedCurrency];
  const activeHighlights = activeCurrencies.flatMap((currency) => {
    const currencyHighlights = highlightsByCurrency.get(currency);
    return currencyHighlights ? [currencyHighlights] : [];
  });
  const displayedSankeyCurrency =
    effectiveSelectedCurrency === "all"
      ? sankeyCurrency && currencies.includes(sankeyCurrency)
        ? sankeyCurrency
        : currencies[0]
      : effectiveSelectedCurrency;
  const displayedSankey = displayedSankeyCurrency
    ? sankeyByCurrency.get(displayedSankeyCurrency)
    : undefined;

  const filteredTransactions = useMemo(() => {
    const q = filterQuery.toLowerCase().trim();
    const currencyTransactions =
      effectiveSelectedCurrency === "all"
        ? transactions
        : transactions.filter((tx) =>
            tx.postings.some(
              (posting) => posting.currency === effectiveSelectedCurrency,
            ),
          );
    if (!q) return currencyTransactions;
    return currencyTransactions.filter(
      (tx) =>
        tx.description.toLowerCase().includes(q) ||
        tx.date.includes(q) ||
        tx.postings.some((p) => p.account.toLowerCase().includes(q)),
    );
  }, [transactions, filterQuery, effectiveSelectedCurrency]);

  const accountRows = useMemo(
    () =>
      Array.from(accounts.values())
        .flatMap((account) =>
          activeCurrencies.flatMap((currency) => {
            const balance = account.balances.get(currency);
            return balance ? [{ account, balance }] : [];
          }),
        )
        .sort((a, b) => {
          if (a.account.rootType !== b.account.rootType)
            return a.account.rootType.localeCompare(b.account.rootType);
          if (a.account.name !== b.account.name)
            return a.account.name.localeCompare(b.account.name);
          return a.balance.currency.localeCompare(b.balance.currency);
        }),
    [accounts, activeCurrencies],
  );

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
      {hasMultipleCurrencies && (
        <div
          aria-label="Journal currencies"
          className="join self-start rounded-lg border border-base-content/10 bg-base-200/50 p-1"
          role="tablist"
        >
          <button
            aria-selected={effectiveSelectedCurrency === "all"}
            className={`btn btn-sm join-item ${effectiveSelectedCurrency === "all" ? "btn-primary" : "btn-ghost"}`}
            onClick={() => setSelectedCurrency("all")}
            role="tab"
            type="button"
          >
            All
          </button>
          {currencies.map((currency) => (
            <button
              aria-selected={effectiveSelectedCurrency === currency}
              className={`btn btn-sm join-item ${effectiveSelectedCurrency === currency ? "btn-primary" : "btn-ghost"}`}
              key={currency}
              onClick={() => setSelectedCurrency(currency)}
              role="tab"
              type="button"
            >
              {currency}
            </button>
          ))}
        </div>
      )}

      {/* 1. Sankey Diagram at Top */}
      <section aria-label="Sankey Diagram Flow">
        {effectiveSelectedCurrency === "all" && hasMultipleCurrencies && (
          <div
            aria-label="Sankey currencies"
            className="join mb-3 rounded-lg border border-base-content/10 bg-base-200/50 p-1"
            role="tablist"
          >
            {currencies.map((currency) => (
              <button
                aria-selected={displayedSankeyCurrency === currency}
                className={`btn btn-xs join-item ${displayedSankeyCurrency === currency ? "btn-secondary" : "btn-ghost"}`}
                key={currency}
                onClick={() => setSankeyCurrency(currency)}
                role="tab"
                type="button"
              >
                {currency} flow
              </button>
            ))}
          </div>
        )}
        <SankeyDiagram
          currency={displayedSankeyCurrency ?? "VND"}
          links={displayedSankey?.links ?? []}
          nodes={displayedSankey?.nodes ?? []}
        />
      </section>

      {/* 2. Key Highlights */}
      <section aria-label="Journal Highlights">
        <JournalHighlights highlights={activeHighlights} />
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
              Transactions ({filteredTransactions.length})
            </button>
            <button
              className={`btn btn-sm join-item ${activeTab === "accounts" ? "btn-primary" : "btn-ghost"}`}
              onClick={() => setActiveTab("accounts")}
              type="button"
            >
              Account Balances ({accountRows.length})
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
                  {(effectiveSelectedCurrency === "all"
                    ? tx.postings
                    : tx.postings.filter(
                        (posting) =>
                          posting.currency === effectiveSelectedCurrency,
                      )
                  ).map((p) => {
                    const accType = classifyAccount(p.account);
                    const isCredit = p.amount < 0;
                    return (
                      <div
                        key={`${tx.id}-${p.account}-${p.amount}-${p.currency}`}
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
                  <th>Currency</th>
                  <th className="text-right">Inflow / Credit</th>
                  <th className="text-right">Outflow / Debit</th>
                  <th className="text-right">Net Balance</th>
                </tr>
              </thead>
              <tbody>
                {accountRows.map(({ account, balance }) => (
                  <tr
                    key={`${account.name}-${balance.currency}`}
                    className="hover:bg-base-200/30"
                  >
                    <td className="font-mono font-medium">{account.name}</td>
                    <td>
                      <span
                        className={`badge badge-xs uppercase ${
                          account.rootType === "income"
                            ? "badge-success"
                            : account.rootType === "expenses"
                              ? "badge-error"
                              : account.rootType === "assets"
                                ? "badge-info"
                                : "badge-neutral"
                        }`}
                      >
                        {account.rootType}
                      </span>
                    </td>
                    <td className="font-mono text-xs text-base-content/60">
                      {balance.currency}
                    </td>
                    <td className="text-right font-mono text-emerald-600 dark:text-emerald-400">
                      {balance.inflow > 0
                        ? formatCurrency(balance.inflow, balance.currency)
                        : "—"}
                    </td>
                    <td className="text-right font-mono text-rose-600 dark:text-rose-400">
                      {balance.outflow > 0
                        ? formatCurrency(balance.outflow, balance.currency)
                        : "—"}
                    </td>
                    <td className="text-right font-mono font-bold">
                      {formatCurrency(balance.net, balance.currency)}
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
