import { describe, expect, it } from "vitest";
import {
  classifyAccount,
  formatCurrency,
  parseAmountAndCurrency,
  parseHledgerJournal,
} from "./parser";

describe("classifyAccount", () => {
  it("classifies income, expenses, assets, liabilities correctly", () => {
    expect(classifyAccount("income:salary:payslip")).toBe("income");
    expect(classifyAccount("Income:Freelance")).toBe("income");
    expect(classifyAccount("expenses:tax:pit")).toBe("expenses");
    expect(classifyAccount("expenses:food:groceries")).toBe("expenses");
    expect(classifyAccount("assets:cash")).toBe("assets");
    expect(classifyAccount("assets:bank:checking")).toBe("assets");
    expect(classifyAccount("liabilities:credit_card")).toBe("liabilities");
    expect(classifyAccount("equity:opening_balance")).toBe("equity");
    expect(classifyAccount("custom:account")).toBe("other");
  });
});

describe("parseAmountAndCurrency", () => {
  it("parses VND with space or suffix", () => {
    expect(parseAmountAndCurrency("20618182 VND")).toEqual({
      amount: 20618182,
      currency: "VND",
    });
    expect(parseAmountAndCurrency("-22909091 VND")).toEqual({
      amount: -22909091,
      currency: "VND",
    });
  });

  it("parses USD with prefix symbol and commas", () => {
    expect(parseAmountAndCurrency("$1,500.50")).toEqual({
      amount: 1500.5,
      currency: "USD",
    });
    expect(parseAmountAndCurrency("-$45.00")).toEqual({
      amount: -45.0,
      currency: "USD",
    });
  });

  it("parses EUR, GBP, JPY", () => {
    expect(parseAmountAndCurrency("€250.00")).toEqual({
      amount: 250,
      currency: "EUR",
    });
    expect(parseAmountAndCurrency("£12.34")).toEqual({
      amount: 12.34,
      currency: "GBP",
    });
    expect(parseAmountAndCurrency("¥10000")).toEqual({
      amount: 10000,
      currency: "JPY",
    });
  });
});

describe("formatCurrency", () => {
  it("formats VND into M notation for millions", () => {
    expect(formatCurrency(20618182, "VND")).toContain("20.62M ₫");
    expect(formatCurrency(5000, "VND")).toBe("5,000 ₫");
  });

  it("formats USD with dollar sign", () => {
    expect(formatCurrency(1234.56, "USD")).toBe("$1,234.56");
  });
});

describe("parseHledgerJournal", () => {
  const samplePayslipJournal = `; Auto-generated from income_payslips.csv
; Amount unit: VND

2020-08-01 Payslip 2020-08
    assets:cash                                  20618182 VND
    expenses:tax:pit                              2290909 VND
    income:salary:payslip                       -22909091 VND

2020-11-01 Payslip 2020-11
    assets:cash                                  36609200 VND
    expenses:tax:pit                              2139800 VND
    expenses:payroll:withholding                  3251000 VND
    income:salary:payslip                       -42000000 VND
`;

  it("parses transactions and postings correctly", () => {
    const result = parseHledgerJournal(samplePayslipJournal);

    expect(result.transactions).toHaveLength(2);
    expect(result.transactions[0]?.date).toBe("2020-08-01");
    expect(result.transactions[0]?.description).toBe("Payslip 2020-08");
    expect(result.transactions[0]?.postings).toHaveLength(3);

    expect(result.transactions[1]?.date).toBe("2020-11-01");
    expect(result.transactions[1]?.postings).toHaveLength(4);
  });

  it("computes highlights and totals accurately", () => {
    const result = parseHledgerJournal(samplePayslipJournal);
    const { highlights } = result;

    // Total income: 22909091 + 42000000 = 64909091
    expect(highlights.totalIncome).toBe(64909091);

    // Total expenses: 2290909 + 2139800 + 3251000 = 7681709
    expect(highlights.totalExpenses).toBe(7681709);

    // Net savings: 64909091 - 7681709 = 57227382
    expect(highlights.netSavings).toBe(57227382);
    expect(highlights.savingsRate).toBeGreaterThan(80);
    expect(highlights.transactionCount).toBe(2);
    expect(highlights.startDate).toBe("2020-08-01");
    expect(highlights.endDate).toBe("2020-11-01");
    expect(highlights.topExpenseCategories.length).toBeGreaterThan(0);
  });

  it("constructs Sankey nodes and links with flow balance", () => {
    const result = parseHledgerJournal(samplePayslipJournal);

    expect(result.sankeyNodes.length).toBeGreaterThan(0);
    expect(result.sankeyLinks.length).toBeGreaterThan(0);

    // Should have income node(s) in column 0
    const col0 = result.sankeyNodes.filter((n) => n.column === 0);
    expect(col0.length).toBeGreaterThan(0);
    expect(col0.some((n) => n.name.includes("salary"))).toBe(true);

    // Should have pool node in column 1
    const col1 = result.sankeyNodes.filter((n) => n.column === 1);
    expect(col1).toHaveLength(1);

    // Should have expense nodes and savings in column 2
    const col2 = result.sankeyNodes.filter((n) => n.column === 2);
    expect(col2.length).toBeGreaterThan(0);
    expect(col2.some((n) => n.type === "savings")).toBe(true);
  });

  it("keeps multi-currency totals, accounts, and flows separate", () => {
    const journal = `2026-10-09 * Banh Mi Huynh Hoa
    expenses:food:dining                  15,000 VND
    assets:momo:wallet                   -15,000 VND

2026-10-09 * Anthropic Claude Pro
    expenses:technology:subscriptions      45.50 USD
    liabilities:credit_cards:vietcombank  -45.50 USD

2026-10-08 * JetBrains All Products Pack
    expenses:technology:software           12.00 USD
    liabilities:credit_cards:vietcombank  -12.00 USD

2026-10-07 * Cloudflare Workers
    expenses:technology:hosting             8.99 USD
    liabilities:credit_cards:vietcombank   -8.99 USD
`;

    const result = parseHledgerJournal(journal);
    const vndHighlights = result.highlightsByCurrency.get("VND");
    const usdHighlights = result.highlightsByCurrency.get("USD");

    expect(result.currencies).toEqual(["VND", "USD"]);
    expect(vndHighlights?.totalExpenses).toBe(15000);
    expect(usdHighlights?.totalExpenses).toBeCloseTo(66.49);
    expect(vndHighlights?.currency).toBe("VND");
    expect(usdHighlights?.currency).toBe("USD");

    expect(
      result.accounts.get("expenses:food:dining")?.balances.get("VND")?.outflow,
    ).toBe(15000);
    expect(
      result.accounts
        .get("expenses:technology:subscriptions")
        ?.balances.get("USD")?.outflow,
    ).toBe(45.5);

    const usdExpenseValues = result.sankeyByCurrency
      .get("USD")
      ?.nodes.filter((node) => node.type === "expense")
      .map((node) => node.value);
    expect(usdExpenseValues).toContain(45.5);
    expect(usdExpenseValues).not.toContain(15000);
  });

  it("balances an omitted posting in its only known currency", () => {
    const result = parseHledgerJournal(`2026-10-09 * USD purchase
    assets:cash                        -100 USD
    expenses:shopping
`);

    const postings = result.transactions[0]?.postings;
    expect(postings?.[1]).toMatchObject({
      amount: 100,
      currency: "USD",
      hasAmount: true,
    });
    expect(result.highlightsByCurrency.get("USD")?.totalExpenses).toBe(100);
  });

  it("does not treat an explicit zero as an omitted amount", () => {
    const result = parseHledgerJournal(`2026-10-09 * Zero expense
    assets:cash                         100 USD
    expenses:shopping                     0 USD
`);

    expect(result.transactions[0]?.postings[1]).toMatchObject({
      amount: 0,
      currency: "USD",
      hasAmount: true,
    });
  });
});
