export interface JournalPosting {
  account: string;
  amount: number;
  currency: string;
  rawText?: string | undefined;
}

export interface JournalTransaction {
  id: string;
  date: string;
  status?: string | undefined;
  description: string;
  postings: JournalPosting[];
  lineNumber: number;
}

export type RootAccountType =
  | "income"
  | "expenses"
  | "assets"
  | "liabilities"
  | "equity"
  | "other";

export interface AccountSummary {
  name: string;
  rootType: RootAccountType;
  inflow: number;
  outflow: number;
  net: number;
  currency: string;
  count: number;
}

export interface SankeyNode {
  id: string;
  name: string;
  column: 0 | 1 | 2; // 0 = Inflow (Income), 1 = Pool/Assets, 2 = Outflow (Expenses)
  value: number;
  currency: string;
  type: "income" | "asset" | "expense" | "pool" | "savings";
}

export interface SankeyLink {
  source: string;
  target: string;
  value: number;
  currency: string;
}

export interface TopCategory {
  name: string;
  amount: number;
  percentage: number;
  currency: string;
}

export interface JournalHighlightsData {
  totalIncome: number;
  totalExpenses: number;
  netSavings: number;
  savingsRate: number;
  transactionCount: number;
  startDate: string | null;
  endDate: string | null;
  currency: string;
  topExpenseCategories: TopCategory[];
}

export interface ParsedJournal {
  transactions: JournalTransaction[];
  accounts: Map<string, AccountSummary>;
  highlights: JournalHighlightsData;
  sankeyNodes: SankeyNode[];
  sankeyLinks: SankeyLink[];
  currencies: string[];
}

export function classifyAccount(accountName: string): RootAccountType {
  const lower = accountName.toLowerCase().trim();
  if (lower.startsWith("income") || lower.startsWith("revenues"))
    return "income";
  if (lower.startsWith("expenses") || lower.startsWith("expense"))
    return "expenses";
  if (lower.startsWith("assets") || lower.startsWith("asset")) return "assets";
  if (lower.startsWith("liabilities") || lower.startsWith("liability"))
    return "liabilities";
  if (lower.startsWith("equity")) return "equity";
  return "other";
}

// Parses amount strings like "20618182 VND", "-22909091 VND", "$50.00", "-$50.00", "1,500.00 USD"
export function parseAmountAndCurrency(
  raw: string,
  defaultCurrency = "VND",
): { amount: number; currency: string } {
  let cleaned = raw.trim();
  let negative = false;

  if (cleaned.startsWith("-")) {
    negative = true;
    cleaned = cleaned.slice(1).trim();
  }

  // Symbol currency prefix like $ or € or £
  let currency = defaultCurrency;
  if (cleaned.startsWith("$")) {
    currency = "USD";
    cleaned = cleaned.slice(1).trim();
  } else if (cleaned.startsWith("€")) {
    currency = "EUR";
    cleaned = cleaned.slice(1).trim();
  } else if (cleaned.startsWith("£")) {
    currency = "GBP";
    cleaned = cleaned.slice(1).trim();
  } else if (cleaned.startsWith("¥")) {
    currency = "JPY";
    cleaned = cleaned.slice(1).trim();
  }

  // Check currency suffix or prefix word
  const matchTrailing = cleaned.match(
    /^([+-]?[\d,]+(?:\.\d+)?)\s*([A-Za-z₫$€£]+)?$/,
  );
  const matchLeading = cleaned.match(
    /^([A-Za-z₫$€£]+)\s*([+-]?[\d,]+(?:\.\d+)?)$/,
  );

  let numStr = "";
  if (matchTrailing) {
    numStr = matchTrailing[1] ?? "";
    if (matchTrailing[2]) currency = matchTrailing[2];
  } else if (matchLeading) {
    if (matchLeading[1]) currency = matchLeading[1];
    numStr = matchLeading[2] ?? "";
  } else {
    // Fallback: extract numbers
    const numMatch = cleaned.match(/[+-]?[\d,]+(?:\.\d+)?/);
    if (numMatch) {
      numStr = numMatch[0];
      const remaining = cleaned.replace(numStr, "").trim();
      if (remaining) currency = remaining;
    }
  }

  // Remove commas used as thousands separators
  const normalizedNum = numStr.replace(/,/g, "");
  let parsedVal = parseFloat(normalizedNum);
  if (Number.isNaN(parsedVal)) {
    parsedVal = 0;
  }
  if (negative) {
    parsedVal = -Math.abs(parsedVal);
  }

  return { amount: parsedVal, currency: currency.trim() || defaultCurrency };
}

export function parseHledgerJournal(content: string): ParsedJournal {
  const lines = content.split(/\r?\n/);
  const transactions: JournalTransaction[] = [];
  const currencyCounts = new Map<string, number>();

  let currentTx: JournalTransaction | null = null;
  let txIndex = 0;

  // Header regex: YYYY[-/.]MM[-/.]DD [*|!]? [Description]
  const dateHeaderRegex = /^(\d{4}[-/.]\d{2}[-/.]\d{2})\s*(?:([*!])\s*)?(.*)$/;

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i] ?? "";
    const trimmed = rawLine.trim();

    if (!trimmed || trimmed.startsWith(";") || trimmed.startsWith("#")) {
      // Empty or full comment line
      continue;
    }

    // Check if line is indented (posting)
    const isIndented = /^\s{2,}|\t/.test(rawLine);

    if (isIndented && currentTx) {
      // It's a posting line
      // Strip comments: ";" or "#"
      const commentIndex = trimmed.search(/[;#]/);
      const postingText =
        commentIndex >= 0 ? trimmed.slice(0, commentIndex).trim() : trimmed;

      if (!postingText) continue;

      // Extract account and amount
      // Account names can contain spaces in some ledgers, but usually separated from amount by 2+ spaces or tab
      const parts = postingText.split(/\s{2,}|\t+/);
      const accountName = parts[0]?.trim() ?? "";

      let parsedAmount = 0;
      let parsedCurrency = "VND";

      if (parts.length > 1 && parts[1]?.trim()) {
        const amountPart = parts.slice(1).join(" ").trim();
        const res = parseAmountAndCurrency(amountPart);
        parsedAmount = res.amount;
        parsedCurrency = res.currency;
        currencyCounts.set(
          parsedCurrency,
          (currencyCounts.get(parsedCurrency) ?? 0) + 1,
        );
      }

      currentTx.postings.push({
        account: accountName,
        amount: parsedAmount,
        currency: parsedCurrency,
        rawText: postingText,
      });
    } else {
      // Check if header line
      const headerMatch = trimmed.match(dateHeaderRegex);
      if (headerMatch) {
        if (currentTx) {
          finalizeTransaction(currentTx);
          transactions.push(currentTx);
        }

        const date = headerMatch[1]?.replace(/[/.]/g, "-") ?? "";
        const status = headerMatch[2];
        let description = headerMatch[3]?.trim() ?? "";
        // Remove trailing comment
        const commentIdx = description.search(/[;#]/);
        if (commentIdx >= 0) {
          description = description.slice(0, commentIdx).trim();
        }

        txIndex++;
        currentTx = {
          id: `tx-${txIndex}-${date}`,
          date,
          status,
          description: description || "Untitled transaction",
          postings: [],
          lineNumber: i + 1,
        };
      }
    }
  }

  if (currentTx) {
    finalizeTransaction(currentTx);
    transactions.push(currentTx);
  }

  // Determine primary currency
  let primaryCurrency = "VND";
  let maxCount = 0;
  for (const [curr, count] of currencyCounts.entries()) {
    if (count > maxCount) {
      maxCount = count;
      primaryCurrency = curr;
    }
  }

  // Calculate accounts summary and highlights
  const accounts = new Map<string, AccountSummary>();
  let totalIncome = 0;
  let totalExpenses = 0;
  let earliestDate: string | null = null;
  let latestDate: string | null = null;

  for (const tx of transactions) {
    if (!earliestDate || tx.date < earliestDate) earliestDate = tx.date;
    if (!latestDate || tx.date > latestDate) latestDate = tx.date;

    for (const post of tx.postings) {
      const type = classifyAccount(post.account);
      let acc = accounts.get(post.account);
      if (!acc) {
        acc = {
          name: post.account,
          rootType: type,
          inflow: 0,
          outflow: 0,
          net: 0,
          currency: post.currency || primaryCurrency,
          count: 0,
        };
        accounts.set(post.account, acc);
      }
      acc.count += 1;
      acc.net += post.amount;

      if (type === "income") {
        // In hledger, income balances are negative (-). Outflow from income account into asset
        const positiveVal = Math.abs(post.amount);
        acc.inflow += positiveVal;
        totalIncome += positiveVal;
      } else if (type === "expenses") {
        // Expenses are positive (+)
        acc.outflow += Math.max(0, post.amount);
        totalExpenses += Math.max(0, post.amount);
      } else if (post.amount > 0) {
        acc.inflow += post.amount;
      } else {
        acc.outflow += Math.abs(post.amount);
      }
    }
  }

  // Calculate top expense categories
  const expenseCategories: { name: string; amount: number }[] = [];
  for (const [name, acc] of accounts.entries()) {
    if (acc.rootType === "expenses" && acc.outflow > 0) {
      expenseCategories.push({ name, amount: acc.outflow });
    }
  }
  expenseCategories.sort((a, b) => b.amount - a.amount);

  const topExpenseCategories: TopCategory[] = expenseCategories
    .slice(0, 5)
    .map((cat) => ({
      name: cat.name,
      amount: cat.amount,
      percentage: totalExpenses > 0 ? (cat.amount / totalExpenses) * 100 : 0,
      currency: primaryCurrency,
    }));

  const netSavings = totalIncome - totalExpenses;
  const savingsRate =
    totalIncome > 0 ? Math.max(0, (netSavings / totalIncome) * 100) : 0;

  const highlights: JournalHighlightsData = {
    totalIncome,
    totalExpenses,
    netSavings,
    savingsRate,
    transactionCount: transactions.length,
    startDate: earliestDate,
    endDate: latestDate,
    currency: primaryCurrency,
    topExpenseCategories,
  };

  // Generate Sankey graph
  const { nodes: sankeyNodes, links: sankeyLinks } = buildSankeyData(
    accounts,
    totalIncome,
    totalExpenses,
    netSavings,
    primaryCurrency,
  );

  return {
    transactions,
    accounts,
    highlights,
    sankeyNodes,
    sankeyLinks,
    currencies: Array.from(currencyCounts.keys()),
  };
}

function finalizeTransaction(tx: JournalTransaction): void {
  // Check for auto-balancing posting (where amount is 0 or omitted)
  const zeroPostings = tx.postings.filter((p) => p.amount === 0);
  if (zeroPostings.length === 1 && tx.postings.length > 1) {
    const sum = tx.postings.reduce((acc, p) => acc + p.amount, 0);
    const zeroPost = zeroPostings[0];
    if (zeroPost) {
      zeroPost.amount = -sum;
    }
  }
}

// Builds the 3-column Sankey flow:
// Column 0: Inflow / Income Sources
// Column 1: Central Account / Liquidity Pool
// Column 2: Outflow / Expenses & Net Retained
function buildSankeyData(
  accounts: Map<string, AccountSummary>,
  totalIncome: number,
  totalExpenses: number,
  netSavings: number,
  currency: string,
): { nodes: SankeyNode[]; links: SankeyLink[] } {
  const nodes: SankeyNode[] = [];
  const links: SankeyLink[] = [];

  const poolId = "pool:liquidity";
  const poolNode: SankeyNode = {
    id: poolId,
    name: "Account Pool / Assets",
    column: 1,
    value: Math.max(totalIncome, totalExpenses),
    currency,
    type: "pool",
  };
  nodes.push(poolNode);

  // Inflow (Column 0)
  const incomeAccounts = Array.from(accounts.values())
    .filter((a) => a.rootType === "income" && a.inflow > 0)
    .sort((a, b) => b.inflow - a.inflow);

  if (incomeAccounts.length > 0) {
    for (const acc of incomeAccounts) {
      const nodeId = `node:${acc.name}`;
      // Display friendly shortened name, e.g. income:salary:payslip -> salary:payslip
      const displayName = acc.name.replace(/^income:/i, "");
      nodes.push({
        id: nodeId,
        name: displayName,
        column: 0,
        value: acc.inflow,
        currency,
        type: "income",
      });

      links.push({
        source: nodeId,
        target: poolId,
        value: acc.inflow,
        currency,
      });
    }
  } else if (totalExpenses > 0) {
    // If no income accounts defined, add "Available Funds / Opening Balance"
    const openingId = "node:opening_balance";
    nodes.push({
      id: openingId,
      name: "Available Funds / Prior Assets",
      column: 0,
      value: totalExpenses,
      currency,
      type: "asset",
    });
    links.push({
      source: openingId,
      target: poolId,
      value: totalExpenses,
      currency,
    });
  }

  // Outflow (Column 2)
  const expenseAccounts = Array.from(accounts.values())
    .filter((a) => a.rootType === "expenses" && a.outflow > 0)
    .sort((a, b) => b.outflow - a.outflow);

  // Group smaller expenses if there are more than 7 to avoid clutter
  const maxExpenses = 6;
  const majorExpenses = expenseAccounts.slice(0, maxExpenses);
  const otherExpenses = expenseAccounts.slice(maxExpenses);

  for (const acc of majorExpenses) {
    const nodeId = `node:${acc.name}`;
    const displayName = acc.name.replace(/^expenses:/i, "");
    nodes.push({
      id: nodeId,
      name: displayName,
      column: 2,
      value: acc.outflow,
      currency,
      type: "expense",
    });

    links.push({
      source: poolId,
      target: nodeId,
      value: acc.outflow,
      currency,
    });
  }

  if (otherExpenses.length > 0) {
    const otherTotal = otherExpenses.reduce((sum, e) => sum + e.outflow, 0);
    const otherId = "node:expenses:other";
    nodes.push({
      id: otherId,
      name: "Other Expenses",
      column: 2,
      value: otherTotal,
      currency,
      type: "expense",
    });

    links.push({
      source: poolId,
      target: otherId,
      value: otherTotal,
      currency,
    });
  }

  // If there is net savings / retained surplus, add "Net Retained / Savings"
  if (netSavings > 0) {
    const savingsId = "node:retained_savings";
    nodes.push({
      id: savingsId,
      name: "Net Retained / Savings",
      column: 2,
      value: netSavings,
      currency,
      type: "savings",
    });

    links.push({
      source: poolId,
      target: savingsId,
      value: netSavings,
      currency,
    });
  }

  return { nodes, links };
}

export function formatCurrency(amount: number, currency = "VND"): string {
  const rounded = Math.round(amount * 100) / 100;
  if (currency === "VND") {
    // Format large VND amounts nicely (e.g., 20.62M VND or formatted number)
    if (Math.abs(rounded) >= 1_000_000) {
      const millions = rounded / 1_000_000;
      return `${millions.toLocaleString(undefined, { minimumFractionDigits: 1, maximumFractionDigits: 2 })}M ₫`;
    }
    return `${rounded.toLocaleString()} ₫`;
  }
  if (currency === "USD") {
    return `$${rounded.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
  }
  return `${rounded.toLocaleString()} ${currency}`;
}
